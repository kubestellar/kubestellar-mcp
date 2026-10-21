//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// writeMultiContextKubeconfig materializes a real kubeconfig file with one
// context per name in contextNames, every one of them pointing at the
// shared envtest apiserver. envtest only ever runs a single control plane,
// so distinct "clusters" here are distinct contexts against the same
// backing server rather than genuinely separate apiservers — that is
// enough to exercise the real per-context client construction and
// concurrent fan-out in pkg/multicluster, which is what this suite is
// after; it does not need the servers to actually differ.
func writeMultiContextKubeconfig(t *testing.T, contextNames ...string) string {
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

	config := clientcmdapi.NewConfig()
	for _, name := range contextNames {
		clusterName := name + "-cluster"
		authName := name + "-auth"
		config.Clusters[clusterName] = cluster
		config.AuthInfos[authName] = authInfo

		kubeContext := clientcmdapi.NewContext()
		kubeContext.Cluster = clusterName
		kubeContext.AuthInfo = authName
		config.Contexts[name] = kubeContext
	}
	config.CurrentContext = contextNames[0]

	path := filepath.Join(t.TempDir(), "multicluster-kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(*config, path), "clientcmd.WriteToFile")

	return path
}

// TestMulticlusterExecutorFanOut exercises pkg/multicluster's ClientManager
// and Executor end-to-end against a real kube-apiserver (envtest): a real
// kubeconfig file on disk, DiscoverClusters enumerating every context in
// it, GetClient building a live *kubernetes.Clientset per context from that
// context's own *rest.Config, and the concurrent executeAll/
// executeAcrossClusters goroutine fan-out collecting one real result per
// cluster. Every other suite in this package (and pkg/multicluster's own
// unit tests) injects a fake ExecuteFunc/fake clientsets straight into
// ClientManager.clients, so this real construction-and-fan-out path has had
// no coverage, unit or integration, until now.
func TestMulticlusterExecutorFanOut(t *testing.T) {
	ctx := context.Background()
	names := []string{"cluster-a", "cluster-b", "cluster-c"}
	kubeconfigPath := writeMultiContextKubeconfig(t, names...)

	manager, err := multicluster.NewClientManager(kubeconfigPath)
	require.NoError(t, err, "NewClientManager")

	clusters, err := manager.DiscoverClusters()
	require.NoError(t, err, "DiscoverClusters")
	require.Lenf(t, clusters, len(names), "DiscoverClusters should find every context: %#v", clusters)

	executor := multicluster.NewExecutor(manager)

	results, err := executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		version, err := client.Discovery().ServerVersion()
		if err != nil {
			return nil, err
		}
		return version.GitVersion, nil
	})
	require.NoError(t, err, `Execute(ctx, "", fn)`)
	require.Lenf(t, results, len(names), "fan-out should produce one result per discovered cluster: %#v", results)

	gotNames := make([]string, 0, len(results))
	for _, r := range results {
		require.Emptyf(t, r.Error, "cluster %s: unexpected fan-out error", r.Cluster)
		require.NotEmptyf(t, r.Result, "cluster %s: empty result", r.Cluster)
		gotNames = append(gotNames, r.Cluster)
	}
	sort.Strings(gotNames)
	wantNames := append([]string(nil), names...)
	sort.Strings(wantNames)
	require.Equal(t, wantNames, gotNames, "fan-out should cover exactly the discovered clusters")

	// Single-cluster mode takes a different branch (executeSingle) than the
	// fan-out above; exercise it too against the same real per-context client.
	single, err := executor.Execute(ctx, names[0], func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		_, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		return "ok", err
	})
	require.NoError(t, err, "Execute single-cluster mode")
	require.Len(t, single, 1)
	require.Equal(t, names[0], single[0].Cluster)
	require.Empty(t, single[0].Error)
	require.Equal(t, "ok", single[0].Result)
}
