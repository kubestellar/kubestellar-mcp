package claude

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errRoundTripper always fails; used to exercise the httpClient.Do
// error branch of sendRequest without a live network.
type errRoundTripper struct{ err error }

func (e errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

// errReader is an io.ReadCloser whose Read always fails; used to
// exercise the io.ReadAll error branch of sendRequest via a
// RoundTripper that returns a response body backed by this reader.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (errReader) Close() error             { return nil }

type bodyErrorRoundTripper struct{}

func (bodyErrorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       errReader{},
		Header:     make(http.Header),
	}, nil
}

// TestQueryReportsRequestBuildError covers the http.NewRequestWithContext
// error branch in sendRequest by pointing baseURL at a byte the URL
// parser rejects.
func TestQueryReportsRequestBuildError(t *testing.T) {
	client := &Client{
		apiKey:     "secret",
		baseURL:    "http://\x7f", // control byte -> parse error
		model:      "claude-test",
		httpClient: http.DefaultClient,
	}
	_, err := client.Query(context.Background(), "sys", "q")
	if err == nil {
		t.Fatal("Query() error = nil; want failed to create request")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Fatalf("Query() error = %v; want 'failed to create request'", err)
	}
}

// TestQueryReportsTransportError covers the httpClient.Do error branch
// via a RoundTripper that always errors.
func TestQueryReportsTransportError(t *testing.T) {
	client := &Client{
		apiKey:     "secret",
		baseURL:    "http://example.invalid",
		model:      "claude-test",
		httpClient: &http.Client{Transport: errRoundTripper{err: errors.New("boom")}},
	}
	_, err := client.Query(context.Background(), "sys", "q")
	if err == nil {
		t.Fatal("Query() error = nil; want failed to send request")
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Fatalf("Query() error = %v; want 'failed to send request'", err)
	}
}

// TestQueryReportsBodyReadError covers the io.ReadAll error branch by
// substituting a body whose Read fails.
func TestQueryReportsBodyReadError(t *testing.T) {
	client := &Client{
		apiKey:     "secret",
		baseURL:    "http://example.invalid",
		model:      "claude-test",
		httpClient: &http.Client{Transport: bodyErrorRoundTripper{}},
	}
	_, err := client.Query(context.Background(), "sys", "q")
	if err == nil {
		t.Fatal("Query() error = nil; want failed to read response")
	}
	if !strings.Contains(err.Error(), "failed to read response") {
		t.Fatalf("Query() error = %v; want 'failed to read response'", err)
	}
}

// TestQueryReportsMalformedSuccessBody covers the json.Unmarshal(&apiResp)
// error branch: a 200 response whose body is not valid JSON.
func TestQueryReportsMalformedSuccessBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-json")
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "secret",
		baseURL:    server.URL,
		model:      "claude-test",
		httpClient: server.Client(),
	}
	_, err := client.Query(context.Background(), "sys", "q")
	if err == nil {
		t.Fatal("Query() error = nil; want failed to parse response")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Fatalf("Query() error = %v; want 'failed to parse response'", err)
	}
}

// TestQueryIgnoresNonTextContent guards the "block.Type == \"text\""
// filter in sendRequest — non-text blocks (e.g., image) must not
// contribute to the concatenated result. This lets a future refactor
// that changes the filter semantics fail loudly instead of silently
// leaking payload metadata into user-visible strings.
func TestQueryIgnoresNonTextContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":[{"type":"image","text":"IMG"},{"type":"text","text":"hi"},{"type":"tool_use","text":"TOOL"}]}`)
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "secret",
		baseURL:    server.URL,
		model:      "claude-test",
		httpClient: server.Client(),
	}
	got, err := client.Query(context.Background(), "sys", "q")
	if err != nil {
		t.Fatalf("Query() unexpected error: %v", err)
	}
	if got != "hi" {
		t.Fatalf("Query() = %q; want %q (non-text blocks must be dropped)", got, "hi")
	}
}
