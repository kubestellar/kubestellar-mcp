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

func TestHandleHelmInstallDiscoversClustersAndPassesFlags(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_UPGRADE_STDOUT", "Release \"demo\" has been upgraded")

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
		"stdin=image:\n  tag: latest",
	} {
		if !strings.Contains(logData, want) {
			t.Fatalf("helm log missing %q in %q", want, logData)
		}
	}
}

func TestHandleHelmUninstallFindsClustersWithExistingRelease(t *testing.T) {
	setupFakeHelm(t)
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "beta")
	t.Setenv("FAKE_HELM_UNINSTALL_STDOUT", "release removed")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	got, err := server.handleHelmUninstall(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"namespace":    "apps",
	}))
	if err != nil {
		t.Fatalf("handleHelmUninstall() error = %v", err)
	}

	result := got.(map[string]interface{})
	clusters := result["targetClusters"].([]string)
	if len(clusters) != 1 || clusters[0] != "beta" {
		t.Fatalf("targetClusters = %v, want [beta]", clusters)
	}
	if result["successCount"].(int) != 1 || result["totalClusters"].(int) != 1 {
		t.Fatalf("unexpected summary fields: %#v", result)
	}
	results := result["results"].([]HelmResult)
	if len(results) != 1 || results[0].Status != "uninstalled" {
		t.Fatalf("unexpected uninstall results: %#v", results)
	}
}

func TestHandleHelmListAggregatesReleasesByCluster(t *testing.T) {
	setupFakeHelm(t)
	t.Setenv("FAKE_HELM_LIST_STDOUT", `[{"name":"demo","namespace":"apps","revision":"1","status":"deployed","chart":"demo-1.0.0","app_version":"1.0.0"}]`)

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	got, err := server.handleHelmList(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"all_namespaces": true,
	}))
	if err != nil {
		t.Fatalf("handleHelmList() error = %v", err)
	}

	result := got.(map[string]interface{})
	clusters := append([]string(nil), result["clusters"].([]string)...)
	sort.Strings(clusters)
	if strings.Join(clusters, ",") != "alpha,beta" {
		t.Fatalf("clusters = %v, want [alpha beta]", clusters)
	}
	if result["totalReleases"].(int) != 2 {
		t.Fatalf("totalReleases = %v, want 2", result["totalReleases"])
	}
	releases := result["releases"].(map[string][]HelmReleaseInfo)
	if len(releases["alpha"]) != 1 || len(releases["beta"]) != 1 {
		t.Fatalf("unexpected releases: %#v", releases)
	}
	if releases["alpha"][0].Name != "demo" || releases["beta"][0].Status != "deployed" {
		t.Fatalf("unexpected release details: %#v", releases)
	}
}

func TestHandleHelmRollbackDryRunTargetsExistingRelease(t *testing.T) {
	logFile := setupFakeHelm(t)
	t.Setenv("FAKE_HELM_STATUS_CLUSTERS", "alpha")
	t.Setenv("FAKE_HELM_ROLLBACK_STDOUT", "rollback preview")

	server := newHelmTestServer(t, map[string]string{
		"alpha": "https://alpha.example.com",
		"beta":  "https://beta.example.com",
	})
	got, err := server.handleHelmRollback(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"release_name": "demo",
		"revision":     3,
		"dry_run":      true,
	}))
	if err != nil {
		t.Fatalf("handleHelmRollback() error = %v", err)
	}

	result := got.(map[string]interface{})
	clusters := result["targetClusters"].([]string)
	if len(clusters) != 1 || clusters[0] != "alpha" {
		t.Fatalf("targetClusters = %v, want [alpha]", clusters)
	}
	results := result["results"].([]HelmResult)
	if len(results) != 1 || results[0].Status != "would-rollback" {
		t.Fatalf("unexpected rollback results: %#v", results)
	}
	logData := readLogFile(t, logFile)
	for _, want := range []string{"cmd=rollback", "cluster=alpha", "args=rollback demo", " 3 --dry-run"} {
		if !strings.Contains(logData, want) {
			t.Fatalf("helm log missing %q in %q", want, logData)
		}
	}
}

func setupFakeHelm(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "fake-helm-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}

	logFile := filepath.Join(absDir, "helm.log")
	t.Setenv("FAKE_HELM_LOG", logFile)
	t.Setenv("PATH", absDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	script := `#!/bin/sh
cmd="$1"
shift
stdin="$(cat)"
cluster=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--kube-context" ]; then
    cluster="$arg"
  fi
  prev="$arg"
done
if [ -n "$FAKE_HELM_LOG" ]; then
  {
    echo "---"
    echo "cmd=$cmd"
    echo "cluster=$cluster"
    echo "args=$cmd $*"
    printf 'stdin=%s\n' "$stdin"
  } >> "$FAKE_HELM_LOG"
fi
case "$cmd" in
  upgrade)
    if [ "$FAKE_HELM_UPGRADE_FAIL" = "1" ]; then
      echo "${FAKE_HELM_UPGRADE_STDERR:-upgrade failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_HELM_UPGRADE_STDOUT:-release upgraded}"
    ;;
  uninstall)
    if [ "$FAKE_HELM_UNINSTALL_FAIL" = "1" ]; then
      echo "${FAKE_HELM_UNINSTALL_STDERR:-uninstall failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_HELM_UNINSTALL_STDOUT:-release removed}"
    ;;
  list)
    if [ "$FAKE_HELM_LIST_FAIL" = "1" ]; then
      exit 1
    fi
    printf '%s' "${FAKE_HELM_LIST_STDOUT:-[]}"
    ;;
  status)
    case ",${FAKE_HELM_STATUS_CLUSTERS}," in
      *,${cluster},*) exit 0 ;;
      *) echo "release not found" >&2; exit 1 ;;
    esac
    ;;
  rollback)
    if [ "$FAKE_HELM_ROLLBACK_FAIL" = "1" ]; then
      echo "${FAKE_HELM_ROLLBACK_STDERR:-rollback failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_HELM_ROLLBACK_STDOUT:-rollback done}"
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
		// Positive case omitted: public hostname resolution requires network in CI.
		// All invalid/blocked inputs must return errors.
		{name: "reject http scheme", repo: "http://charts.example.com", wantErr: true},
		{name: "reject file scheme", repo: "file:///etc/passwd", wantErr: true},
		{name: "reject ssh scheme", repo: "ssh://git@github.com/charts", wantErr: true},
		{name: "reject empty", repo: "", wantErr: true},
		{name: "reject no host", repo: "https://", wantErr: true},
		{name: "reject loopback IP", repo: "https://127.0.0.1/charts", wantErr: true},
		{name: "reject localhost", repo: "https://localhost/charts", wantErr: true},
		{name: "reject private 10.x", repo: "https://10.0.0.1/charts", wantErr: true},
		{name: "reject private 192.168.x", repo: "https://192.168.1.1/charts", wantErr: true},
		{name: "reject cloud metadata", repo: "https://169.254.169.254/latest", wantErr: true},
		{name: "reject CGNAT 100.64.x", repo: "https://100.64.0.1/charts", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHelmRepoURL(tt.repo)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHelmRepoURL(%q) error = %v, wantErr %v", tt.repo, err, tt.wantErr)
			}
		})
	}
}
