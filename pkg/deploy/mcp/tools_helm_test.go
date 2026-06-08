package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
	"github.com/kubestellar/kubestellar-mcp/pkg/multicluster"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// setHelmMockResolver installs a stub DNS resolver for the duration of the test
// so that validateHelmRepoURL does not perform real network calls.
func setHelmMockResolver(t *testing.T, resolve func(host string) (addrs []string, err error)) {
	t.Helper()
	orig := helmHostResolver
	helmHostResolver = resolve
	t.Cleanup(func() { helmHostResolver = orig })
}

func TestHandleHelmInstallDiscoversClustersAndPassesFlags(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_UPGRADE_STDOUT", "Release \"demo\" has been upgraded")
	// Stub DNS so validateHelmRepoURL does not require network access.
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"93.184.216.34"}, nil // public IP — not blocked
	})

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"chart":        "oci://example/demo",
		"namespace":    "apps",
		"values":       map[string]string{"replicas": "2"},
		"values_yaml":  "image:\n  tag: latest\n",
		"version":      "1.2.3",
		"repo":         "https://charts.example.com",
		"wait":         true,
		"timeout":      "5m",
		"dry_run":      true,
	})

	got, err := server.handleHelmInstall(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmInstall() error = %v", err)
	}

	result := got.(map[string]interface{})
	clusters := append([]string(nil), result["targetClusters"].([]string)...)
	sort.Strings(clusters)
	if strings.Join(clusters, ",") != "alpha,beta" {
		t.Fatalf("targetClusters = %v, want [alpha beta]", clusters)
	}
	if result["successCount"].(int) != 2 || result["totalClusters"].(int) != 2 || !result["dryRun"].(bool) {
		t.Fatalf("unexpected summary fields: %#v", result)
	}

	results := result["results"].([]HelmResult)
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Status != "would-install" {
			t.Fatalf("unexpected install result: %#v", r)
		}
	}

	logData := readLogFile(t, logFile)
	for _, want := range []string{
		"cmd=upgrade",
		"--repo https://charts.example.com",
		"--version 1.2.3",
		"--set replicas=2",
		"--values -",
		"--wait",
		"--timeout 5m",
		"--dry-run",
		"cluster=alpha",
		"cluster=beta",
	} {
		if !strings.Contains(logData, want) {
			t.Errorf("log missing %q", want)
		}
	}
}

func TestHandleHelmUninstallFindsClustersWithExistingRelease(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "gamma")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"gamma": "https://gamma.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "myrelease",
		"namespace":    "default",
		"dry_run":      true,
	})

	got, err := server.handleHelmUninstall(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmUninstall() error = %v", err)
	}

	result := got.(map[string]interface{})
	if result["successCount"].(int) != 1 || result["totalClusters"].(int) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	results := result["results"].([]HelmResult)
	if len(results) != 1 || results[0].Status != "would-uninstall" {
		t.Fatalf("unexpected result status: %#v", results)
	}
	_ = logFile
}

func TestHandleHelmListAggregatesReleasesByCluster(t *testing.T) {
	_ = setupFakeHelm(t)
	t.Setenv("FAKE_HELM_LIST_JSON", `[{"name":"myapp","namespace":"default","revision":"1","status":"deployed","chart":"myapp-1.0","app_version":"1.0"}]`)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"namespace": "default",
	})

	got, err := server.handleHelmList(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmList() error = %v", err)
	}

	result := got.(map[string]interface{})
	if result["totalReleases"].(int) != 2 {
		t.Fatalf("totalReleases = %d, want 2", result["totalReleases"].(int))
	}
	releases := result["releases"].(map[string][]HelmReleaseInfo)
	for _, cluster := range []string{"alpha", "beta"} {
		if len(releases[cluster]) != 1 || releases[cluster][0].Name != "myapp" {
			t.Errorf("releases[%s] = %#v, want [{Name:myapp}]", cluster, releases[cluster])
		}
	}
}

func TestHandleHelmRollbackDryRunTargetsExistingRelease(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "alpha")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	args := mustMarshalJSON(t, map[string]interface{}{
		"release_name": "webapp",
		"namespace":    "production",
		"revision":     3,
		"dry_run":      true,
	})

	got, err := server.handleHelmRollback(context.Background(), args)
	if err != nil {
		t.Fatalf("handleHelmRollback() error = %v", err)
	}

	result := got.(map[string]interface{})
	if result["successCount"].(int) != 1 || result["totalClusters"].(int) != 1 {
		t.Fatalf("unexpected rollback result: %#v", result)
	}
	results := result["results"].([]HelmResult)
	if len(results) != 1 || results[0].Status != "would-rollback" {
		t.Fatalf("unexpected result status: %#v", results)
	}

	logData := readLogFile(t, logFile)
	for _, want := range []string{
		"cmd=rollback",
		"--namespace production",
		"--kube-context alpha",
		"--dry-run",
		"cluster=alpha",
	} {
		if !strings.Contains(logData, want) {
			t.Errorf("log missing %q", want)
		}
	}
}

func setupFakeHelm(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "fake-helm-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir) //nolint:errcheck
	})

	absDir, err := filepath.Abs(tmpDir)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	t.Setenv("PATH", absDir+":"+os.Getenv("PATH"))

	logFile := filepath.Join(absDir, "helm.log")
	t.Setenv("FAKE_HELM_LOG", logFile)

	script := `#!/bin/bash
set -euo pipefail

cmd="$1"
shift

# Capture all args
echo "cmd=${cmd}" >> "${FAKE_HELM_LOG:-/dev/null}"
echo "args=$@" >> "${FAKE_HELM_LOG:-/dev/null}"

# Extract cluster context and namespace from args
prev=""
for i in "$@"; do
  case "$prev" in
    --kube-context) echo "cluster=${i}" >> "${FAKE_HELM_LOG:-/dev/null}" ;;
    --namespace|-n) echo "namespace=${i}" >> "${FAKE_HELM_LOG:-/dev/null}" ;;
  esac
  prev="$i"
done

case "$cmd" in
  upgrade)
    echo "${FAKE_HELM_UPGRADE_STDOUT:-Release \"demo\" has been installed}"
    ;;
  uninstall)
    echo "release \"$(echo "$@" | awk '{print $1}')\" uninstalled"
    ;;
  list)
    echo "${FAKE_HELM_LIST_JSON:-[]}"
    ;;
  rollback)
    echo "Rollback was a success! Happy Helming!"
    ;;
  status)
    # Check if release should exist
    CLUSTER=$(prev=""; for i in "$@"; do case "$prev" in --kube-context) echo "$i";; esac; prev="$i"; done)
    if echo "${FAKE_HELM_STATUS_CLUSTERS:-}" | grep -qw "$CLUSTER"; then
      echo "STATUS: deployed"
    else
      echo "Error: release not found" >&2
      exit 1
    fi
    ;;
  *)
    echo "unsupported command: $cmd" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(absDir, "helm"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return logFile
}

func newHelmTestServer(t *testing.T, contexts map[string]string) *Server {
	t.Helper()

	config := clientcmdapi.NewConfig()
	firstContext := ""
	for name, serverURL := range contexts {
		if firstContext == "" {
			firstContext = name
		}
		config.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
		config.Clusters[name] = &clientcmdapi.Cluster{Server: serverURL}
		config.AuthInfos[name] = &clientcmdapi.AuthInfo{}
	}
	config.CurrentContext = firstContext

	dir, err := os.MkdirTemp(".", "helm-kubeconfig-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	manager, err := multicluster.NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("NewClientManager() error = %v", err)
	}

	executor := multicluster.NewExecutor(manager)
	selector := multicluster.NewSelector(executor)

	return &Server{
		manager:  manager,
		executor: executor,
		selector: selector,
		newManifestReader: func() *gitops.ManifestReader {
			return gitops.NewManifestReaderWithSchemes(map[string]bool{
				"https": true,
				"http":  true,
				"file":  true,
			})
		},
	}
}

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}

func readLogFile(t *testing.T, logFile string) string {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(data)
}

func TestValidateHelmRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		// Scheme checks — no DNS needed for these.
		{name: "reject http scheme", repo: "http://charts.example.com", wantErr: true},
		{name: "reject file scheme", repo: "file:///etc/passwd", wantErr: true},
		{name: "reject ssh scheme", repo: "ssh://git@github.com/charts", wantErr: true},
		{name: "reject empty", repo: "", wantErr: true},
		{name: "reject no host", repo: "https://", wantErr: true},
		// IP-literal checks — no DNS needed because the host is already an IP.
		{name: "reject loopback IP", repo: "https://127.0.0.1/charts", wantErr: true},
		{name: "reject private 10.x", repo: "https://10.0.0.1/charts", wantErr: true},
		{name: "reject private 192.168.x", repo: "https://192.168.1.1/charts", wantErr: true},
		{name: "reject cloud metadata", repo: "https://169.254.169.254/latest", wantErr: true},
		{name: "reject CGNAT", repo: "https://100.64.0.1/charts", wantErr: true},
		// Public IP literal — allowed without DNS lookup.
		{name: "allow public IP literal", repo: "https://93.184.216.34/charts", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Use a resolver that should never be called for IP-literal tests.
			// For scheme/host tests it also won't be reached.
			setHelmMockResolver(t, func(host string) ([]string, error) {
				t.Errorf("unexpected DNS lookup for host %q", host)
				return nil, nil
			})
			err := validateHelmRepoURL(tc.repo)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateHelmRepoURL(%q) error = %v, wantErr %v", tc.repo, err, tc.wantErr)
			}
		})
	}
}

func TestValidateHelmRepoURL_DNSBlocked(t *testing.T) {
	// Simulate a domain that resolves to a private IP (SSRF via DNS rebinding).
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"10.0.0.1"}, nil // private IP
	})
	err := validateHelmRepoURL("https://evil.example.com/charts")
	if err == nil {
		t.Error("expected error when domain resolves to private IP, got nil")
	}
}

func TestValidateHelmRepoURL_DNSPublic(t *testing.T) {
	// Simulate a domain that resolves to a public IP.
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"93.184.216.34"}, nil // public IP
	})
	err := validateHelmRepoURL("https://charts.example.com/charts")
	if err != nil {
		t.Errorf("expected no error for public IP, got %v", err)
	}
}

func TestValidateHelmRepoURL_localhost(t *testing.T) {
	// Simulate localhost resolving to 127.0.0.1 (as it does on most systems).
	setHelmMockResolver(t, func(_ string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	})
	err := validateHelmRepoURL("https://localhost/charts")
	if err == nil {
		t.Error("expected error for localhost resolving to loopback, got nil")
	}
}
