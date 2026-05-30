package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	if _, err := NewClient(); err == nil {
		t.Fatal("NewClient() expected error when ANTHROPIC_API_KEY is missing")
	}
}

func TestNewClientAppliesOptions(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	customHTTPClient := &http.Client{}

	client, err := NewClient(
		WithBaseURL("https://example.test"),
		WithModel("claude-test"),
		WithHTTPClient(customHTTPClient),
	)
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	if client.baseURL != "https://example.test" || client.model != "claude-test" || client.httpClient != customHTTPClient {
		t.Fatalf("client options not applied: %#v", client)
	}
}

func TestQuerySendsExpectedRequest(t *testing.T) {
	var captured Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/messages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "secret" {
			t.Fatalf("x-api-key = %q, want secret", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("anthropic-version = %q", got)
		}
		decodeClaudeRequest(t, r, &captured)
		_ = json.NewEncoder(w).Encode(Response{
			Content: []ContentBlock{{Type: "text", Text: "hello "}, {Type: "image", Text: "ignored"}, {Type: "text", Text: "world"}},
		})
	}))
	defer server.Close()

	client := &Client{apiKey: "secret", baseURL: server.URL, model: "claude-test", httpClient: server.Client()}
	got, err := client.Query(context.Background(), "system prompt", "why?")
	if err != nil {
		t.Fatalf("Query() unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("Query() = %q, want hello world", got)
	}
	if captured.Model != "claude-test" || captured.System != "system prompt" || len(captured.Messages) != 1 || captured.Messages[0].Content != "why?" {
		t.Fatalf("unexpected request payload: %#v", captured)
	}
}

func TestChatSendsAllMessages(t *testing.T) {
	var captured Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeClaudeRequest(t, r, &captured)
		_ = json.NewEncoder(w).Encode(Response{Content: []ContentBlock{{Type: "text", Text: "ok"}}})
	}))
	defer server.Close()

	client := &Client{apiKey: "secret", baseURL: server.URL, model: "claude-test", httpClient: server.Client()}
	messages := []Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	if _, err := client.Chat(context.Background(), "system", messages); err != nil {
		t.Fatalf("Chat() unexpected error: %v", err)
	}
	if len(captured.Messages) != len(messages) {
		t.Fatalf("captured %d messages, want %d", len(captured.Messages), len(messages))
	}
}

func TestQueryHandlesAPIErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{name: "json error", statusCode: http.StatusBadRequest, body: `{"error":{"type":"invalid_request","message":"bad prompt"}}`, want: "API error: invalid_request - bad prompt"},
		{name: "plain text error", statusCode: http.StatusInternalServerError, body: "boom", want: "API error (status 500): boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := &Client{apiKey: "secret", baseURL: server.URL, model: "claude-test", httpClient: server.Client()}
			_, err := client.Query(context.Background(), "system", "question")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Query() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestQueryHandlesEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{Content: []ContentBlock{}})
	}))
	defer server.Close()

	client := &Client{apiKey: "secret", baseURL: server.URL, model: "claude-test", httpClient: server.Client()}
	_, err := client.Query(context.Background(), "system", "question")
	if err == nil || !strings.Contains(err.Error(), "empty response from API") {
		t.Fatalf("Query() error = %v, want empty response error", err)
	}
}

func decodeClaudeRequest(t *testing.T, r *http.Request, target *Request) {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		t.Fatalf("failed to decode request: %v", err)
	}
}
