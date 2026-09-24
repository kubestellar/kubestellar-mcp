package helm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
    RELEASE_NAME="$1"
    CLUSTER=$(prev=""; for i in "$@"; do case "$prev" in --kube-context) echo "$i";; esac; prev="$i"; done)
    if echo "${FAKE_HELM_UNINSTALL_FAIL_CLUSTERS:-}" | grep -qw "$CLUSTER"; then
      echo "${FAKE_HELM_UNINSTALL_FAIL_MSG:-Error: uninstall failed for ${RELEASE_NAME}}" >&2
      exit 1
    fi
    echo "release \"${RELEASE_NAME}\" uninstalled"
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

// newHelmTestServer builds a *Server backed by a real
// *multicluster.ClientManager (constructed from an in-memory kubeconfig
// covering the given contexts), mirroring the pre-refactor
// newHelmTestServer fixture in pkg/deploy/mcp.
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

	dir := t.TempDir()

	kubeconfig := filepath.Join(dir, "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	manager, err := multicluster.NewClientManager(kubeconfig)
	if err != nil {
		t.Fatalf("NewClientManager() error = %v", err)
	}

	return &Server{Access: manager}
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
