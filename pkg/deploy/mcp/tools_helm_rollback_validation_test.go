package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestHandleHelmRollbackValidationErrors covers three previously-uncovered
// argument-validation branches at the top of handleHelmRollback:
//
//   - invalid JSON payload (json.Unmarshal error)
//   - empty release_name (required check)
//   - invalid release_name identifier (flag injection guard #269)
//   - invalid cluster name (validateHelmClusters guard #289)
//
// These all short-circuit before any helm subprocess exec, so no fake-helm
// setup is needed.
func TestHandleHelmRollbackValidationErrors(t *testing.T) {
	s := &Server{}

	cases := []struct {
		name string
		args json.RawMessage
		want string
	}{
		{
			name: "invalid json",
			args: json.RawMessage(`not json`),
			want: "invalid arguments",
		},
		{
			name: "missing release_name",
			args: json.RawMessage(`{"namespace":"default"}`),
			want: "release_name is required",
		},
		{
			name: "release_name with flag injection",
			args: json.RawMessage(`{"release_name":"--set=evil","namespace":"default"}`),
			want: "release_name",
		},
		{
			name: "cluster name with flag injection",
			args: json.RawMessage(`{"release_name":"demo","namespace":"default","clusters":["--kube-context=steal"]}`),
			want: "cluster",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.handleHelmRollback(context.Background(), tc.args)
			if err == nil {
				t.Fatalf("handleHelmRollback() error = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("handleHelmRollback() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
