package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// These tests drive the main `watchUpgrade` render loop (the ~30 statements
// from the `for { select { … } }` down to `return nil` on complete) end-to-end
// against a real dynamic client backed by an httptest server. Previously the
// only watchUpgrade coverage was the three pre-loop error paths in
// watch_test.go, so `go tool cover -func` reported the loop at 23.7%. These
// tests exercise:
//
//   - the happy-path early return via `if status.Complete { return nil }`,
//   - the `<-ctx.Done()` branch that prints "Stopped watching." and returns nil,
//   - the `getUpgradeStatus` error branch that logs to stderr and continues
//     until ctx cancels.
//
// No production code is changed — everything lives in this test file, using
// only the existing `configFlags.KubeConfig` seam. The pattern is
// intentionally lightweight so it can be reused when more upgrade CLI
// commands land in this package.

// writeKubeconfigFor writes a minimal kubeconfig pointing at the given
// httptest server URL and returns its path. `insecure-skip-tls-verify: true`
// keeps the fake HTTP server usable without wiring TLS.
func writeKubeconfigFor(t *testing.T, dir, server string) string {
	t.Helper()

	path := filepath.Join(dir, "kubeconfig")
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: fake
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: fake
  context:
    cluster: fake
    user: fake
current-context: fake
users:
- name: fake
  user:
    token: fake-token
`, server)

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// clusterVersionJSON returns a marshalled ClusterVersion object whose
// Progressing condition drives `getUpgradeStatus` toward the desired outcome.
//   - complete=true → message contains "Cluster version is <version>" so
//     `getUpgradeStatus` sets Complete=true, Percent=100.
//   - complete=false → message is a mid-upgrade progress string.
func clusterVersionJSON(t *testing.T, complete bool) []byte {
	t.Helper()

	msg := "Working towards 4.18.30: 168 of 906 done (18% complete), waiting on kube-apiserver"
	if complete {
		msg = "Cluster version is 4.18.30"
	}

	body := map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]interface{}{"name": "version"},
		"status": map[string]interface{}{
			"desired": map[string]interface{}{"version": "4.18.30"},
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "Progressing",
					"status":  "True",
					"message": msg,
				},
			},
		},
	}
	buf, err := json.Marshal(body)
	require.NoError(t, err)
	return buf
}

// TestWatchUpgrade_Loop_CompletesAndReturnsNil drives the loop through its
// happy-path `if status.Complete { return nil }` branch. The httptest server
// returns a ClusterVersion whose Progressing message matches
// `Cluster version is <desired>`, so `getUpgradeStatus` yields
// Complete=true and the loop returns nil on the first tick after
// ensureOpenShiftCluster succeeds.
func TestWatchUpgrade_Loop_CompletesAndReturnsNil(t *testing.T) {
	body := clusterVersionJSON(t, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/config.openshift.io/v1/clusterversions/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	kc := writeKubeconfigFor(t, dir, srv.URL)
	explicit := kc
	ctxName := ""
	configFlags := &genericclioptions.ConfigFlags{
		KubeConfig: &explicit,
		Context:    &ctxName,
	}

	// Generous ceiling — the happy path should return well under this.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := watchUpgrade(ctx, configFlags, 5*time.Millisecond)

	require.NoError(t, err, "loop must return nil via the Complete branch")
	require.NoError(t, ctx.Err(), "loop must return before the ctx deadline")
}

// TestWatchUpgrade_Loop_CanceledContextReturnsNil drives the loop through
// its `<-ctx.Done()` branch. The server returns a non-complete ClusterVersion
// forever; we cancel ctx after a couple of ticks. The loop must return nil
// (not an error) after printing "Stopped watching.".
func TestWatchUpgrade_Loop_CanceledContextReturnsNil(t *testing.T) {
	body := clusterVersionJSON(t, false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/config.openshift.io/v1/clusterversions/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	kc := writeKubeconfigFor(t, dir, srv.URL)
	explicit := kc
	ctxName := ""
	configFlags := &genericclioptions.ConfigFlags{
		KubeConfig: &explicit,
		Context:    &ctxName,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := watchUpgrade(ctx, configFlags, 5*time.Millisecond)

	require.NoError(t, err, "ctx cancel must return nil, not an error")
}

// TestWatchUpgrade_Loop_StatusFetchErrorContinues drives the loop through its
// `if err != nil { fmt.Fprintf(os.Stderr, ...) }` branch. The server serves
// the first Get (the ensureOpenShiftCluster precheck) successfully but
// returns 500 for every subsequent Get. The loop must swallow the error,
// keep polling, and eventually return nil when ctx cancels — proving the
// error branch does not abort the watcher.
func TestWatchUpgrade_Loop_StatusFetchErrorContinues(t *testing.T) {
	ok := clusterVersionJSON(t, false)
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/config.openshift.io/v1/clusterversions/version" {
			http.NotFound(w, r)
			return
		}
		n := hits.Add(1)
		if n == 1 {
			// Precheck: succeed so watchUpgrade enters the loop.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(ok)
			return
		}
		// Every subsequent Get inside the loop must fail so the loop
		// takes the error branch and continues, not the render branch.
		http.Error(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"boom","code":500}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	kc := writeKubeconfigFor(t, dir, srv.URL)
	explicit := kc
	ctxName := ""
	configFlags := &genericclioptions.ConfigFlags{
		KubeConfig: &explicit,
		Context:    &ctxName,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := watchUpgrade(ctx, configFlags, 5*time.Millisecond)

	require.NoError(t, err, "in-loop Get errors must not abort the watcher")
	// Precheck (1) plus at least one loop-time error tick must have run.
	assert.GreaterOrEqual(t, int(hits.Load()), 2,
		"loop must poll at least once after the precheck")
}
