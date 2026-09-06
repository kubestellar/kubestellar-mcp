package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

// Covers the first-return `json.Marshal(rawObj)` error path of each of
// applyDeployment / applyService / applyConfigMap / applySecret. json.Marshal
// on `map[string]interface{}` fails when the map contains a value that has
// no JSON representation — a channel value is the canonical trigger.
//
// Previously these lines (tools_deploy.go:262-263, :303-304, :344-345, :385-386)
// showed 0 hits in coverage: apply* was reported at 86.4%.

type applyFn func(*Server, context.Context, kubernetes.Interface, map[string]interface{}, string) (string, error)

func applyMarshalErrCases() []struct {
	name string
	apply applyFn
} {
	return []struct {
		name  string
		apply applyFn
	}{
		{"deployment", func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
			return s.applyDeployment(ctx, c, raw, ns)
		}},
		{"service", func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
			return s.applyService(ctx, c, raw, ns)
		}},
		{"configmap", func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
			return s.applyConfigMap(ctx, c, raw, ns)
		}},
		{"secret", func(s *Server, ctx context.Context, c kubernetes.Interface, raw map[string]interface{}, ns string) (string, error) {
			return s.applySecret(ctx, c, raw, ns)
		}},
	}
}

// TestApplyResourceFunctions_MarshalErrorPropagates ensures the first
// json.Marshal error return in each apply* helper is exercised. Using a
// channel value in the raw object makes json.Marshal fail synchronously
// with "json: unsupported type: chan int".
func TestApplyResourceFunctions_MarshalErrorPropagates(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})
	badRaw := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]interface{}{"name": "demo"},
		// unsupported type — makes json.Marshal fail immediately.
		"__bad": make(chan int),
	}
	for _, tc := range applyMarshalErrCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := kubernetesfake.NewSimpleClientset()
			_, err := tc.apply(server, context.Background(), client, badRaw, "default")
			require.Error(t, err)
			assert.True(t,
				strings.Contains(err.Error(), "unsupported type") ||
					strings.Contains(err.Error(), "chan"),
				"expected json marshal error mentioning chan/unsupported, got: %v", err)
		})
	}
}
