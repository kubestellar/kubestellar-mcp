package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// setupFakeKustomize and createTestKustomization are retained in the root
// package because server_protocol_test.go and server_test.go (cross-domain
// protocol dispatch tests, not kustomize-domain files) still exercise the
// kustomize_build tool end-to-end via Server.handleToolCall. The
// kustomize-specific unit tests that used to live alongside these fixtures
// moved to pkg/deploy/mcp/kustomize/ as part of epic #983 phase 1; this file
// is an unmodified copy of the fixtures they still share.
func setupFakeKustomize(t *testing.T) string {
	t.Helper()

	absDir := t.TempDir()

	logFile := filepath.Join(absDir, "kustomize.log")
	t.Setenv("FAKE_KUSTOMIZE_LOG", logFile)
	t.Setenv("PATH", absDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	kustomizeScript := `#!/bin/sh
cmd="$1"
shift
if [ -n "$FAKE_KUSTOMIZE_LOG" ]; then
  {
    echo "---"
    echo "bin=kustomize"
    echo "args=$cmd $*"
  } >> "$FAKE_KUSTOMIZE_LOG"
fi
case "$cmd" in
  build)
    if [ "$FAKE_KUSTOMIZE_BUILD_FAIL" = "1" ]; then
      echo "${FAKE_KUSTOMIZE_BUILD_STDERR:-kustomize build failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUSTOMIZE_BUILD_STDOUT:-kind: ConfigMap\n}"
    ;;
  *)
    echo "unsupported kustomize command: $cmd" >&2
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(absDir, "kustomize"), []byte(kustomizeScript), 0o755))

	kubectlScript := `#!/bin/sh
cmd="$1"
shift
stdin="$(cat)"
cluster=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--context" ] || [ "$prev" = "--kube-context" ]; then
    cluster="$arg"
  fi
  prev="$arg"
done
if [ -n "$FAKE_KUSTOMIZE_LOG" ]; then
  {
    echo "---"
    echo "bin=kubectl"
    echo "cmd=$cmd"
    echo "cluster=$cluster"
    echo "args=$cmd $*"
    printf 'stdin=%s\n' "$stdin"
  } >> "$FAKE_KUSTOMIZE_LOG"
fi
case "$cmd" in
  kustomize)
    printf '%s' "${FAKE_KUBECTL_KUSTOMIZE_STDOUT:-kind: ConfigMap\n}"
    ;;
  apply)
    if [ "$FAKE_KUBECTL_APPLY_FAIL" = "1" ]; then
      echo "${FAKE_KUBECTL_APPLY_STDERR:-kubectl apply failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUBECTL_APPLY_STDOUT:-applied}"
    ;;
  delete)
    if [ "$FAKE_KUBECTL_DELETE_FAIL" = "1" ]; then
      echo "${FAKE_KUBECTL_DELETE_STDERR:-kubectl delete failed}" >&2
      exit 1
    fi
    printf '%s' "${FAKE_KUBECTL_DELETE_STDOUT:-deleted}"
    ;;
  *)
    echo "unsupported kubectl command: $cmd" >&2
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(absDir, "kubectl"), []byte(kubectlScript), 0o755))

	return logFile
}

func createTestKustomization(t *testing.T, filename string) string {
	t.Helper()

	dir := t.TempDir()

	content := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644))

	return dir
}
