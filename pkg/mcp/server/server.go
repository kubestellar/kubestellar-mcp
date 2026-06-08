package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"k8s.io/client-go/kubernetes"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

const (
	ServerName    = "kubestellar-ops"
	ServerVersion = "0.8.0"
)

type discoverer interface {
	DiscoverClusters(source string) ([]cluster.ClusterInfo, error)
	CheckHealthByContext(contextName string) (*cluster.HealthInfo, error)
}

// Server implements an MCP server over stdio
type Server struct {
	kubeconfig    string
	discoverer    discoverer
	clientFactory func(clusterName string) (kubernetes.Interface, error)
	reader        *bufio.Reader
	writer        io.Writer
	mu            sync.Mutex
}

// Type aliases — re-export from pkg/mcp/protocol for backward compatibility
// within this package. Other packages should import protocol directly.
type (
	Request          = protocol.Request
	Response         = protocol.Response
	Error            = protocol.Error
	ServerInfo       = protocol.ServerInfo
	InitializeResult = protocol.InitializeResult
	Capabilities     = protocol.Capabilities
	ToolsCapability  = protocol.ToolsCapability
	Tool             = protocol.Tool
	InputSchema      = protocol.InputSchema
	Property         = protocol.Property
	Items            = protocol.Items
	ToolsListResult  = protocol.ToolsListResult
	CallToolParams   = protocol.CallToolParams
	CallToolResult   = protocol.CallToolResult
	ContentBlock     = protocol.ContentBlock
)
