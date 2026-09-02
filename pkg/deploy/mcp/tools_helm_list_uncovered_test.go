package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleHelmListRejectsInvalidJSONArgs covers the first uncovered arm of
// handleHelmList: json.Unmarshal failure on malformed arguments.
func TestHandleHelmListRejectsInvalidJSONArgs(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	_, err := server.handleHelmList(context.Background(), json.RawMessage(`{"namespace": "default",`))
	if err == nil {
		t.Fatal("expected error for malformed JSON args, got nil")
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Errorf("error = %q, want to contain 'invalid arguments'", err.Error())
	}
}

// TestHandleHelmListRejectsInvalidClusterName covers validateHelmClusters
// error arm: a cluster name beginning with '-' is treated as flag injection.
func TestHandleHelmListRejectsInvalidClusterName(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"clusters": []string{"-badflag"},
	})
	_, err := server.handleHelmList(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for cluster name starting with '-', got nil")
	}
	if !strings.Contains(err.Error(), "flag injection") {
		t.Errorf("error = %q, want to contain 'flag injection'", err.Error())
	}
}

// TestHandleHelmListRejectsInvalidNamespaceIdentifier covers the
// validateHelmIdentifier("namespace", ...) error arm — a namespace beginning
// with '-' is rejected as possible flag injection.
func TestHandleHelmListRejectsInvalidNamespaceIdentifier(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"namespace": "-injected",
	})
	_, err := server.handleHelmList(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for namespace starting with '-', got nil")
	}
	if !strings.Contains(err.Error(), "flag injection") {
		t.Errorf("error = %q, want to contain 'flag injection'", err.Error())
	}
}

// TestHandleHelmListRejectsFilterFlagInjection covers the filter-with-hyphen
// arm of handleHelmList.
func TestHandleHelmListRejectsFilterFlagInjection(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})

	args := mustMarshalJSON(t, map[string]interface{}{
		"filter": "-rm-rf",
	})
	_, err := server.handleHelmList(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for filter starting with '-', got nil")
	}
	if !strings.Contains(err.Error(), "flag injection") {
		t.Errorf("error = %q, want to contain 'flag injection'", err.Error())
	}
}

// TestHelmListAppendsAllNamespacesFlag covers the allNs=true arm of helmList
// via handleHelmList, verifying "--all-namespaces" reaches the helm CLI.
func TestHelmListAppendsAllNamespacesFlag(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_LIST_JSON", `[]`)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"all_namespaces": true,
	})

	if _, err := server.handleHelmList(context.Background(), args); err != nil {
		t.Fatalf("handleHelmList() error = %v", err)
	}

	log := readLogFile(t, logFile)
	if !strings.Contains(log, "--all-namespaces") {
		t.Errorf("expected '--all-namespaces' in helm args; log:\n%s", log)
	}
}

// TestHelmListAppendsFilterFlag covers the filter != "" arm of helmList by
// providing a benign filter value and asserting it reaches the helm CLI.
func TestHelmListAppendsFilterFlag(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_LIST_JSON", `[]`)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"namespace": "default",
		"filter":    "myapp.*",
	})

	if _, err := server.handleHelmList(context.Background(), args); err != nil {
		t.Fatalf("handleHelmList() error = %v", err)
	}

	log := readLogFile(t, logFile)
	if !strings.Contains(log, "--filter myapp.*") {
		t.Errorf("expected '--filter myapp.*' in helm args; log:\n%s", log)
	}
}

// installFailingHelm writes a fake `helm` on PATH that exits non-zero for
// `list` (triggering the cmd.Run() error arm) and returns success for
// `status` so upstream cluster discovery still works when needed.
func installFailingHelm(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  list) echo 'boom' >&2; exit 1;;\n  status) exit 0;;\n  *) exit 0;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// installBadJSONHelm writes a fake `helm` on PATH that prints invalid JSON on
// `list`, exercising the json.Unmarshal error arm of helmList.
func installBadJSONHelm(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  list) echo 'not-json{';;\n  *) exit 0;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// TestHelmListReturnsNilOnCommandFailure covers the cmd.Run() error arm.
func TestHelmListReturnsNilOnCommandFailure(t *testing.T) {
	installFailingHelm(t)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"namespace": "default",
	})

	got, err := server.handleHelmList(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmList() error = %v", err)
	}
	result := got.(map[string]interface{})
	if result["totalReleases"].(int) != 0 {
		t.Errorf("totalReleases = %d, want 0 when helm list fails",
			result["totalReleases"].(int))
	}
	releases := result["releases"].(map[string][]HelmReleaseInfo)
	if len(releases) != 0 {
		t.Errorf("releases = %#v, want empty when helm list fails", releases)
	}
}

// TestHelmListReturnsNilOnUnmarshalFailure covers the json.Unmarshal error arm.
func TestHelmListReturnsNilOnUnmarshalFailure(t *testing.T) {
	installBadJSONHelm(t)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"namespace": "default",
	})

	got, err := server.handleHelmList(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmList() error = %v", err)
	}
	result := got.(map[string]interface{})
	if result["totalReleases"].(int) != 0 {
		t.Errorf("totalReleases = %d, want 0 when helm list returns bad JSON",
			result["totalReleases"].(int))
	}
}
