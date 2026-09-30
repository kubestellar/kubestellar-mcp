package diagnostics

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// testServer mirrors the subset of pkg/mcp/server.Server fields the moved
// diagnostics tests configure, so they keep their original shape.
type testServer struct {
	clientFactory func(clusterName string) (kubernetes.Interface, error)
}

func (s *testServer) deps() *handlers.Deps {
	return &handlers.Deps{
		ClientFactory: s.clientFactory,
	}
}

// newTestRegistry returns a fresh registry populated only by this package's
// Register, so tests never depend on pkg/mcp/server's global registry.
func newTestRegistry() *handlers.Registry {
	reg := handlers.NewRegistry()
	Register(reg)
	return reg
}
