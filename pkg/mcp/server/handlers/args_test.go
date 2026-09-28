package handlers

import (
	"strings"
	"testing"
)

func TestExtractAndValidateNamespace(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantNS  string
		wantErr string // substring; empty means no error expected
	}{
		{
			name:   "missing namespace key returns all-namespaces sentinel",
			args:   map[string]interface{}{},
			wantNS: "",
		},
		{
			name:   "nil args behaves like missing key",
			args:   nil,
			wantNS: "",
		},
		{
			name:   "valid namespace passes through",
			args:   map[string]interface{}{"namespace": "team-alpha"},
			wantNS: "team-alpha",
		},
		{
			name:    "non-string namespace is rejected",
			args:    map[string]interface{}{"namespace": 42},
			wantErr: "namespace must be a string",
		},
		{
			name:    "non-string bool namespace is rejected",
			args:    map[string]interface{}{"namespace": true},
			wantErr: "namespace must be a string",
		},
		{
			name:    "empty string namespace fails validator",
			args:    map[string]interface{}{"namespace": ""},
			wantErr: "namespace",
		},
		{
			name:    "system namespace kube-system is rejected",
			args:    map[string]interface{}{"namespace": "kube-system"},
			wantErr: "kube-system",
		},
		{
			name:    "RFC 1123 violation is rejected",
			args:    map[string]interface{}{"namespace": "BadNS"},
			wantErr: "BadNS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractAndValidateNamespace(tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.wantNS {
					t.Fatalf("got %q, want %q", got, tt.wantNS)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (result=%q)", tt.wantErr, got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
			if got != "" {
				t.Fatalf("on error path, expected empty result, got %q", got)
			}
		})
	}
}
