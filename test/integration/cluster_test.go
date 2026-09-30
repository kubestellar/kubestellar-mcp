//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterdiscovery "github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	clustertools "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
)

// writeIntegrationKubeconfig materializes a real kubeconfig file pointing at
// the shared envtest apiserver, with integrationClusterName as its single
// (and current) context. The cluster-domain tools go through
// pkg/cluster.Discoverer, which reads kubeconfig from disk via clientcmd
// loading rules, so a file — not a *rest.Config — is what the tools need.
//
// Both the inline-data and the file-path forms of envtest's client
// credentials are copied over, because envtest may hand back either
// depending on how the control plane was started.
func writeIntegrationKubeconfig(t *testing.T) string {
	t.Helper()

	cluster := clientcmdapi.NewCluster()
	cluster.Server = testCfg.Host
	cluster.CertificateAuthorityData = testCfg.CAData
	cluster.CertificateAuthority = testCfg.CAFile
	if len(cluster.CertificateAuthorityData) == 0 && cluster.CertificateAuthority == "" {
		cluster.InsecureSkipTLSVerify = true
	}

	authInfo := clientcmdapi.NewAuthInfo()
	authInfo.ClientCertificateData = testCfg.CertData
	authInfo.ClientKeyData = testCfg.KeyData
	authInfo.ClientCertificate = testCfg.CertFile
	authInfo.ClientKey = testCfg.KeyFile
	authInfo.Token = testCfg.BearerToken
	authInfo.Username = testCfg.Username
	authInfo.Password = testCfg.Password

	kubeContext := clientcmdapi.NewContext()
	kubeContext.Cluster = integrationClusterName
	kubeContext.AuthInfo = integrationClusterName

	config := clientcmdapi.NewConfig()
	config.Clusters[integrationClusterName] = cluster
	config.AuthInfos[integrationClusterName] = authInfo
	config.Contexts[integrationClusterName] = kubeContext
	config.CurrentContext = integrationClusterName

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(*config, path), "clientcmd.WriteToFile")

	return path
}

// newClusterRegistry mirrors pkg/mcp/server/cluster_tools.go: it copies
// cluster.Tools() into a handlers.Registry in registration order, which is
// exactly how the real protocol server exposes the cluster domain.
func newClusterRegistry() *handlers.Registry {
	reg := handlers.NewRegistry()
	for _, td := range clustertools.Tools() {
		reg.Register(td.Schema, td.Handler)
	}
	return reg
}

// TestClusterTools exercises pkg/mcp/server/cluster's list_clusters and
// get_cluster_health MCP tools end-to-end against a real kube-apiserver
// (envtest). Unlike the package's own unit tests, which inject a stub
// Discoverer, this suite writes a real kubeconfig for the envtest control
// plane and wires the real pkg/cluster.Discoverer behind handlers.Deps, so
// the whole path is live: clientcmd loading rules → context enumeration →
// TLS client construction → Discovery().ServerVersion() → Nodes().List().
//
// envtest runs no kubelet, so the cluster has zero nodes; that is the
// readyCount == totalCount == 0 branch of CheckHealth, which reports a
// healthy apiserver with "0/0" nodes ready.
func TestClusterTools(t *testing.T) {
	ctx := context.Background()

	kubeconfigPath := writeIntegrationKubeconfig(t)
	reg := newClusterRegistry()
	deps := &handlers.Deps{
		Kubeconfig: kubeconfigPath,
		Discoverer: clusterdiscovery.NewDiscoverer(kubeconfigPath),
	}

	tests := []struct {
		name           string
		tool           string
		args           map[string]interface{}
		wantError      bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "list_clusters reports the envtest context as current",
			tool: "list_clusters",
			args: map[string]interface{}{},
			wantContains: []string{
				"Discovered clusters:",
				"- " + integrationClusterName + " (current)",
				"Source: kubeconfig",
				"Server: " + testCfg.Host,
			},
		},
		{
			name: "list_clusters honors an explicit kubeconfig source",
			tool: "list_clusters",
			args: map[string]interface{}{"source": "kubeconfig"},
			wantContains: []string{
				"- " + integrationClusterName + " (current)",
				"Source: kubeconfig",
			},
		},
		{
			name:         "list_clusters rejects the unimplemented kubestellar source",
			tool:         "list_clusters",
			args:         map[string]interface{}{"source": "kubestellar"},
			wantError:    true,
			wantContains: []string{"Failed to discover clusters", "not yet implemented"},
		},
		{
			name: "get_cluster_health reports the current context as healthy",
			tool: "get_cluster_health",
			args: map[string]interface{}{},
			wantContains: []string{
				"Cluster: " + integrationClusterName,
				"Status: Healthy",
				"API Server: Healthy",
				"Nodes Ready: 0/0",
			},
		},
		{
			name: "get_cluster_health resolves a cluster named explicitly",
			tool: "get_cluster_health",
			args: map[string]interface{}{"cluster": integrationClusterName},
			wantContains: []string{
				"Cluster: " + integrationClusterName,
				"API Server: Healthy",
			},
		},
		{
			name:         "get_cluster_health reports an unknown cluster as not found",
			tool:         "get_cluster_health",
			args:         map[string]interface{}{"cluster": "mcp-integration-absent-cluster"},
			wantError:    true,
			wantContains: []string{`"mcp-integration-absent-cluster" not found`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := reg.Find(tt.tool)
			require.NotNilf(t, handler, "%s tool not found in registry", tt.tool)

			output, isError := handler(ctx, deps, tt.args)
			require.Equalf(t, tt.wantError, isError, "%s isError mismatch, output: %s", tt.tool, output)

			for _, want := range tt.wantContains {
				require.Containsf(t, output, want, "%s output missing %q", tt.tool, want)
			}
			for _, unwanted := range tt.wantNotContain {
				require.NotContainsf(t, output, unwanted, "%s output unexpectedly mentions %q", tt.tool, unwanted)
			}
		})
	}
}
