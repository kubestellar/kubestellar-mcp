package policy

import (
	"context"
	"testing"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// stubDiscoverer satisfies handlers.Discoverer; the policy handlers never
// call it, but the tests moved here from pkg/mcp/server still set it (they
// were originally constructed against pkg/mcp/server.Server, which requires
// a non-nil discoverer for the shared dispatch path).
type stubDiscoverer struct{}

func (stubDiscoverer) DiscoverClusters(string) ([]cluster.ClusterInfo, error) { return nil, nil }

func (stubDiscoverer) CheckHealthByContext(string) (*cluster.HealthInfo, error) {
	return nil, nil
}

// testServer mirrors the subset of pkg/mcp/server.Server fields the moved
// policy tests configure, so they keep their original shape after being
// moved into this package.
type testServer struct {
	discoverer           handlers.Discoverer
	clientFactory        func(clusterName string) (kubernetes.Interface, error)
	dynamicClientFactory func(clusterName string) (dynamic.Interface, error)
}

func (s *testServer) deps() *handlers.Deps {
	return &handlers.Deps{
		Discoverer:           s.discoverer,
		ClientFactory:        s.clientFactory,
		DynamicClientFactory: s.dynamicClientFactory,
	}
}

// newTestRegistry returns a fresh registry populated only by this package's
// Register, so tests never depend on pkg/mcp/server's global registry.
func newTestRegistry() *handlers.Registry {
	reg := handlers.NewRegistry()
	Register(reg)
	return reg
}

// callTool dispatches tool through a Register-populated registry against s
// and wraps the handler's (text, isError) return in the same
// protocol.CallToolResult shape pkg/mcp/server sends. An unregistered tool
// name is reported as a JSON-RPC -32602 error, matching pkg/mcp/server's
// handleToolsCall.
func callTool(t *testing.T, s *testServer, tool string, args map[string]interface{}) (protocol.CallToolResult, *protocol.Error) {
	t.Helper()

	handler := newTestRegistry().Find(tool)
	if handler == nil {
		return protocol.CallToolResult{}, &protocol.Error{Code: -32602, Message: "Unknown tool: " + tool}
	}

	text, isError := handler(context.Background(), s.deps(), args)
	return protocol.CallToolResult{
		Content: []protocol.ContentBlock{{Type: "text", Text: text}},
		IsError: isError,
	}, nil
}
