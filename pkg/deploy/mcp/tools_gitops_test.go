package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHandleDetectDriftValidation(t *testing.T) {
	server := newHelmTestServer(t, nil)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{
			name:    "missing repo",
			args:    map[string]interface{}{},
			wantErr: "repo is required",
		},
		{
			name:    "empty repo",
			args:    map[string]interface{}{"repo": ""},
			wantErr: "repo is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := mustMarshalJSON(t, tt.args)
			_, err := server.handleDetectDrift(context.Background(), args)
			if err == nil {
				t.Fatalf("handleDetectDrift() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handleDetectDrift() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandleSyncFromGitValidation(t *testing.T) {
	server := newHelmTestServer(t, nil)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr string
	}{
		{
			name:    "missing repo",
			args:    map[string]interface{}{},
			wantErr: "repo is required",
		},
		{
			name:    "empty repo",
			args:    map[string]interface{}{"repo": ""},
			wantErr: "repo is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := mustMarshalJSON(t, tt.args)
			_, err := server.handleSyncFromGit(context.Background(), args)
			if err == nil {
				t.Fatalf("handleSyncFromGit() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handleSyncFromGit() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandleReconcileValidation(t *testing.T) {
	server := newHelmTestServer(t, nil)

	args := mustMarshalJSON(t, map[string]interface{}{})

	_, err := server.handleReconcile(context.Background(), args)
	if err == nil {
		t.Fatalf("handleReconcile() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "repo is required") {
		t.Fatalf("handleReconcile() error = %v, want error containing 'repo is required'", err)
	}
}

func TestHandlePreviewChangesValidation(t *testing.T) {
	server := newHelmTestServer(t, nil)

	args := mustMarshalJSON(t, map[string]interface{}{})

	_, err := server.handlePreviewChanges(context.Background(), args)
	if err == nil {
		t.Fatalf("handlePreviewChanges() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "repo is required") {
		t.Fatalf("handlePreviewChanges() error = %v, want error containing 'repo is required'", err)
	}
}

func TestHandleReconcileDelegation(t *testing.T) {
	server := newHelmTestServer(t, nil)

	// Test that handleReconcile properly delegates to handleSyncFromGit with dry_run=false
	args := mustMarshalJSON(t, map[string]interface{}{
		"repo":      "https://example.com/repo",
		"path":      "manifests",
		"branch":    "main",
		"namespace": "apps",
	})

	// Both should fail the same way since reconcile delegates to sync
	_, reconcileErr := server.handleReconcile(context.Background(), args)
	_, syncErr := server.handleSyncFromGit(context.Background(), args)

	if reconcileErr == nil || syncErr == nil {
		t.Fatalf("expected both to error (git read would fail)")
	}

	// Error messages should be similar since reconcile delegates to sync
	if !strings.Contains(reconcileErr.Error(), "failed to read manifests from git") {
		t.Fatalf("handleReconcile() error = %v, want error about reading manifests", reconcileErr)
	}
}

func TestHandlePreviewChangesDelegation(t *testing.T) {
	server := newHelmTestServer(t, nil)

	// Test that handlePreviewChanges properly delegates to handleSyncFromGit with dry_run=true
	args := mustMarshalJSON(t, map[string]interface{}{
		"repo":      "https://example.com/repo",
		"path":      "manifests",
		"branch":    "main",
		"namespace": "apps",
	})

	// Both should fail the same way since preview delegates to sync
	_, previewErr := server.handlePreviewChanges(context.Background(), args)
	_, syncErr := server.handleSyncFromGit(context.Background(), args)

	if previewErr == nil || syncErr == nil {
		t.Fatalf("expected both to error (git read would fail)")
	}

	// Error messages should be similar since preview delegates to sync
	if !strings.Contains(previewErr.Error(), "failed to read manifests from git") {
		t.Fatalf("handlePreviewChanges() error = %v, want error about reading manifests", previewErr)
	}
}

func TestHandleSyncFromGitInvalidArguments(t *testing.T) {
	server := newHelmTestServer(t, nil)

	// Invalid JSON should fail to unmarshal
	_, err := server.handleSyncFromGit(context.Background(), json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatalf("handleSyncFromGit() expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("handleSyncFromGit() error = %v, want error containing 'invalid arguments'", err)
	}
}

func TestHandleDetectDriftInvalidArguments(t *testing.T) {
	server := newHelmTestServer(t, nil)

	// Invalid JSON should fail to unmarshal
	_, err := server.handleDetectDrift(context.Background(), json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatalf("handleDetectDrift() expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("handleDetectDrift() error = %v, want error containing 'invalid arguments'", err)
	}
}

func TestHandleReconcileInvalidArguments(t *testing.T) {
	server := newHelmTestServer(t, nil)

	// Invalid JSON should fail to unmarshal
	_, err := server.handleReconcile(context.Background(), json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatalf("handleReconcile() expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("handleReconcile() error = %v, want error containing 'invalid arguments'", err)
	}
}

func TestHandlePreviewChangesInvalidArguments(t *testing.T) {
	server := newHelmTestServer(t, nil)

	// Invalid JSON should fail to unmarshal
	_, err := server.handlePreviewChanges(context.Background(), json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatalf("handlePreviewChanges() expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("handlePreviewChanges() error = %v, want error containing 'invalid arguments'", err)
	}
}
