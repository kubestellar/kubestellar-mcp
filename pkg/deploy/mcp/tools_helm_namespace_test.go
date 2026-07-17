package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestHandleHelmNamespaceValidation covers the security-relevant rejection
// branch in the four helm handlers where server.ValidateNamespace is called
// after arg parsing but before any cluster interaction (see #377). Each case
// asserts the "invalid namespace" wrapper prefix so future refactors of the
// wrapping message still fail loud.
func TestHandleHelmNamespaceValidation(t *testing.T) {
	s := &Server{}

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "install blocked system namespace",
			call: func() error {
				_, err := s.handleHelmInstall(context.Background(), json.RawMessage(`{"release_name":"demo","chart":"nginx","namespace":"kube-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "install openshift-prefixed namespace",
			call: func() error {
				_, err := s.handleHelmInstall(context.Background(), json.RawMessage(`{"release_name":"demo","chart":"nginx","namespace":"openshift-monitoring"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "install invalid namespace format",
			call: func() error {
				_, err := s.handleHelmInstall(context.Background(), json.RawMessage(`{"release_name":"demo","chart":"nginx","namespace":"Invalid_NS"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "uninstall blocked system namespace",
			call: func() error {
				_, err := s.handleHelmUninstall(context.Background(), json.RawMessage(`{"release_name":"demo","namespace":"kube-public"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "uninstall gatekeeper-system namespace",
			call: func() error {
				_, err := s.handleHelmUninstall(context.Background(), json.RawMessage(`{"release_name":"demo","namespace":"gatekeeper-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "list blocked system namespace",
			call: func() error {
				_, err := s.handleHelmList(context.Background(), json.RawMessage(`{"namespace":"kube-node-lease"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "list openshift-prefixed namespace",
			call: func() error {
				_, err := s.handleHelmList(context.Background(), json.RawMessage(`{"namespace":"openshift-logging"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "rollback blocked system namespace",
			call: func() error {
				_, err := s.handleHelmRollback(context.Background(), json.RawMessage(`{"release_name":"demo","namespace":"kube-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "rollback invalid namespace format",
			call: func() error {
				_, err := s.handleHelmRollback(context.Background(), json.RawMessage(`{"release_name":"demo","namespace":"Invalid_NS"}`))
				return err
			},
			want: "invalid namespace",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
