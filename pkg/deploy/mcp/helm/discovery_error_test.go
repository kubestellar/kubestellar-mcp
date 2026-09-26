package helm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
)

// failingAccess is a ClusterAccess whose DiscoverClusters always fails. The
// helm handlers fall back to discovery only when the caller omits `clusters`,
// so each handler has a dedicated "discovery failed" return that the
// kubeconfig-backed fixture (newHelmTestServer) can never reach.
type failingAccess struct{ err error }

func (f failingAccess) DiscoverClusters() ([]multicluster.ClusterInfo, error) {
	return nil, f.err
}

// assertHelmNotInvoked fails the test if the fake helm wrote a log, i.e. if
// any helm subprocess was spawned. The fake only creates the log on first
// invocation, so a missing file is the expected state.
func assertHelmNotInvoked(t *testing.T, logFile string) {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if err == nil {
		t.Fatalf("helm must not be invoked; log = %q", string(data))
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error reading helm log: %v", err)
	}
}

// TestHandlers_DiscoverClustersErrorPropagates exercises the DiscoverClusters
// error branch in all four helm handlers. No helm subprocess must be spawned:
// the error is returned before any per-cluster work begins.
func TestHandlers_DiscoverClustersErrorPropagates(t *testing.T) {
	logFile := setupFakeHelm(t)
	setHelmMockResolver(t, func(string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	})
	discoveryErr := errors.New("kubeconfig unreadable")
	server := &Server{Access: failingAccess{err: discoveryErr}}

	tests := []struct {
		name    string
		handler func(context.Context, json.RawMessage) (interface{}, error)
		args    map[string]interface{}
	}{
		{
			name:    "install",
			handler: server.handleHelmInstall,
			args: map[string]interface{}{
				"release_name": "demo",
				"chart":        "bitnami/nginx",
				"repo":         "https://charts.example.com",
			},
		},
		{
			name:    "list",
			handler: server.handleHelmList,
			args:    map[string]interface{}{"namespace": "apps"},
		},
		{
			name:    "rollback",
			handler: server.handleHelmRollback,
			args:    map[string]interface{}{"release_name": "demo", "revision": 1},
		},
		{
			name:    "uninstall",
			handler: server.handleHelmUninstall,
			args:    map[string]interface{}{"release_name": "demo"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.handler(context.Background(), mustMarshalJSON(t, tc.args))
			if !errors.Is(err, discoveryErr) {
				t.Fatalf("%s error = %v, want %v", tc.name, err, discoveryErr)
			}
			if got != nil {
				t.Fatalf("%s result = %v, want nil on discovery failure", tc.name, got)
			}
		})
	}

	assertHelmNotInvoked(t, logFile)
}

// TestHandleHelmUninstall_RejectsInvalidClusterName covers the
// validateHelmClusters guard in handleHelmUninstall (#289), which runs after
// the identifier checks and before discovery.
func TestHandleHelmUninstall_RejectsInvalidClusterName(t *testing.T) {
	logFile := setupFakeHelm(t)
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"clusters":     []string{"--kube-context=evil"},
	})
	_, err := server.handleHelmUninstall(context.Background(), args)
	if err == nil {
		t.Fatal("handleHelmUninstall() error = nil, want cluster-name validation error")
	}
	if !strings.Contains(err.Error(), "cluster") {
		t.Fatalf("error = %q, want it to mention the offending cluster name", err)
	}
	assertHelmNotInvoked(t, logFile)
}

// TestValidateHelmRepoURL_UnparseableURL covers the url.Parse failure branch:
// a control character in the URL is rejected by net/url before any scheme or
// host checks run.
func TestValidateHelmRepoURL_UnparseableURL(t *testing.T) {
	err := validateHelmRepoURL("https://charts.example.com/\x7f")
	if err == nil {
		t.Fatal("validateHelmRepoURL() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "invalid repo URL") {
		t.Fatalf("error = %q, want 'invalid repo URL' prefix", err)
	}
}

// TestRevalidateHelmHosts_StripsPortFromOCIHost covers the host:port branch of
// revalidateHelmHosts: the port must be stripped before the host is resolved,
// otherwise the resolver would be handed "ghcr.io:5000" and fail.
func TestRevalidateHelmHosts_StripsPortFromOCIHost(t *testing.T) {
	var resolved []string
	setHelmMockResolver(t, func(host string) ([]string, error) {
		resolved = append(resolved, host)
		return []string{"93.184.216.34"}, nil
	})

	if err := revalidateHelmHosts("oci://ghcr.io:5000/org/chart:1.0", ""); err != nil {
		t.Fatalf("revalidateHelmHosts() error = %v, want nil", err)
	}
	if len(resolved) != 1 || resolved[0] != "ghcr.io" {
		t.Fatalf("resolver received %v, want [ghcr.io] (port stripped)", resolved)
	}
}

// TestResolveAndBlock_DNSFailure covers the resolver-error branch of
// resolveAndBlock: a lookup failure must be surfaced (fail closed), not
// treated as "no blocked IPs found".
func TestResolveAndBlock_DNSFailure(t *testing.T) {
	lookupErr := errors.New("no such host")
	setHelmMockResolver(t, func(string) ([]string, error) {
		return nil, lookupErr
	})

	err := resolveAndBlock("missing.example.com")
	if !errors.Is(err, lookupErr) {
		t.Fatalf("resolveAndBlock() error = %v, want wrapped %v", err, lookupErr)
	}
	if !strings.Contains(err.Error(), "DNS lookup failed") {
		t.Fatalf("error = %q, want 'DNS lookup failed' prefix", err)
	}
}
