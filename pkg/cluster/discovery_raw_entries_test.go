package cluster

import (
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestDiscoverRawClusterEntriesSkipsMissingCluster covers
// DiscoverRawClusterEntries' `!ok` skip arm, both with onSkipped nil (the
// silent-skip path used by discoverFromKubeconfig) and with onSkipped set,
// to confirm the callback fires with the orphaned context's name and its
// missing cluster reference. Neither branch was previously exercised from
// this package's own test suite: the only caller in pkg/cluster always
// passes nil, and the non-nil onSkipped arm was only ever reached via
// pkg/multicluster's tests, which do not count toward pkg/cluster's
// coverage.
func TestDiscoverRawClusterEntriesSkipsMissingCluster(t *testing.T) {
	config := clientcmdapi.NewConfig()
	config.CurrentContext = "good"
	config.Contexts["good"] = &clientcmdapi.Context{Cluster: "good"}
	config.Clusters["good"] = &clientcmdapi.Cluster{Server: "https://good.example.com"}
	config.Contexts["orphan"] = &clientcmdapi.Context{Cluster: "missing"}

	t.Run("nil onSkipped silently skips", func(t *testing.T) {
		entries := DiscoverRawClusterEntries(*config, nil)
		if len(entries) != 1 {
			t.Fatalf("entries = %#v, want 1 entry", entries)
		}
		if got := entries[0]; got.ContextName != "good" || got.Server != "https://good.example.com" || !got.Current {
			t.Fatalf("unexpected entry: %#v", got)
		}
	})

	t.Run("non-nil onSkipped is invoked for the orphan", func(t *testing.T) {
		var skippedContext, skippedCluster string
		calls := 0
		entries := DiscoverRawClusterEntries(*config, func(contextName, clusterRef string) {
			calls++
			skippedContext = contextName
			skippedCluster = clusterRef
		})

		if len(entries) != 1 {
			t.Fatalf("entries = %#v, want 1 entry", entries)
		}
		if calls != 1 {
			t.Fatalf("onSkipped called %d times, want 1", calls)
		}
		if skippedContext != "orphan" || skippedCluster != "missing" {
			t.Fatalf("onSkipped(%q, %q), want (%q, %q)", skippedContext, skippedCluster, "orphan", "missing")
		}
	})
}
