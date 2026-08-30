package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes several branches of applyManifestDynamic and parseYAML
// in pkg/deploy/mcp/tools_kubectl.go that go tool cover reports as
// uncovered:
//
//   * applyManifestDynamic: the s.manager.GetConfig(clusterName) error
//     branch (lines 319-320) — fires when the caller names a cluster
//     that does not exist in the multicluster kubeconfig.
//   * applyManifestDynamic: the real (non-dryRun) create branch
//     (lines 397-424 in the current file) — the GET-then-POST sequence
//     against the dynamic client for a resource that does not yet exist,
//     and the parallel GET-then-PUT update branch for a resource that
//     already exists.
//   * applyManifestDynamic: the "unknown resource kind" fallback (empty
//     GVR) end-to-end at the top level (not just inside the mid-stream
//     helper).
//   * parseYAML: the yamlToJSONBytes error return (lines 524-525) — hit
//     when the input is malformed YAML that k8syaml.ToJSON refuses.

// TestApplyManifestDynamic_UnknownClusterReturnsError exercises the
// s.manager.GetConfig(clusterName) failure branch.
func TestApplyManifestDynamic_UnknownClusterReturnsError(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{"alpha": "https://alpha.example.com"})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: default
data:
  key: value`

	results, err := server.applyManifestDynamic(context.Background(), "does-not-exist", manifest, false)
	require.Error(t, err)
	assert.Nil(t, results)
	// Error wraps the cluster name and comes from the GetConfig failure.
	assert.Contains(t, err.Error(), "failed to get config for cluster does-not-exist")
}

// TestApplyManifestDynamic_UnknownKindEndToEnd verifies the empty-GVR
// fallback appends a per-document failure rather than aborting the whole
// call, and that the message names the offending kind. This is exercised
// only in non-dryRun mode because dryRun short-circuits earlier.
func TestApplyManifestDynamic_UnknownKindEndToEnd(t *testing.T) {
	// A dummy httptest server is fine — dynClient.Resource() with an empty
	// GVR is never invoked, so no HTTP traffic occurs. But the dynamic
	// client still needs a URL that dialing succeeds against.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected request "+r.URL.Path, http.StatusInternalServerError)
	}))
	t.Cleanup(fake.Close)

	server := newHelmTestServer(t, map[string]string{"c1": fake.URL})

	manifest := `apiVersion: example.com/v1
kind: MadeUpResource
metadata:
  name: nope
  namespace: default`

	results, err := server.applyManifestDynamic(context.Background(), "c1", manifest, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].Status)
	assert.Contains(t, results[0].Message, "unknown resource kind")
	assert.Contains(t, results[0].Message, "MadeUpResource")
	// Result is stamped with the cluster/kind/name for auditability.
	assert.Equal(t, "c1", results[0].Cluster)
	assert.Equal(t, "MadeUpResource", results[0].Kind)
	assert.Equal(t, "nope", results[0].Name)
}

// fakeConfigMap is the tiny subset of ConfigMap fields the fake API needs
// to round-trip. Using a generic map keeps the fake decoupled from any
// specific corev1 struct import.
type fakeK8sState struct {
	mu     sync.Mutex
	stored map[string]map[string]interface{} // name -> object
}

// startFakeConfigMapAPI returns an httptest.Server that mimics just
// enough of the k8s REST surface to satisfy the dynamic client's
// GET (name-lookup) and POST (create) / PUT (update) for the
// core-v1 ConfigMap resource in the "default" namespace.
func startFakeConfigMapAPI(t *testing.T, state *fakeK8sState) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Match /api/v1/namespaces/default/configmaps and
		// /api/v1/namespaces/default/configmaps/<name>.
		const listPath = "/api/v1/namespaces/default/configmaps"
		p := r.URL.Path

		if p == listPath {
			// Only POST (create) uses the collection URL.
			if r.Method != http.MethodPost {
				http.Error(w, "unexpected method "+r.Method, http.StatusMethodNotAllowed)
				return
			}
			var obj map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			meta, _ := obj["metadata"].(map[string]interface{})
			name, _ := meta["name"].(string)

			state.mu.Lock()
			if state.stored == nil {
				state.stored = map[string]map[string]interface{}{}
			}
			// Assign a resourceVersion so subsequent updates work.
			if meta == nil {
				meta = map[string]interface{}{}
				obj["metadata"] = meta
			}
			meta["resourceVersion"] = "1"
			state.stored[name] = obj
			state.mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(obj)
			return
		}

		if strings.HasPrefix(p, listPath+"/") {
			name := strings.TrimPrefix(p, listPath+"/")
			state.mu.Lock()
			obj, ok := state.stored[name]
			state.mu.Unlock()

			switch r.Method {
			case http.MethodGet:
				if !ok {
					// Return the standard 404 body so the dynamic client
					// treats it as a "not found" (not a transport error)
					// and falls through to the create branch.
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"kind":       "Status",
						"apiVersion": "v1",
						"status":     "Failure",
						"reason":     "NotFound",
						"code":       404,
					})
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(obj)
				return
			case http.MethodPut:
				var updated map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				state.mu.Lock()
				meta, _ := updated["metadata"].(map[string]interface{})
				if meta == nil {
					meta = map[string]interface{}{}
					updated["metadata"] = meta
				}
				meta["resourceVersion"] = "2"
				state.stored[name] = updated
				state.mu.Unlock()
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(updated)
				return
			default:
				http.Error(w, "unexpected method "+r.Method, http.StatusMethodNotAllowed)
				return
			}
		}

		http.Error(w, "unexpected path "+p, http.StatusNotFound)
	}))
}

// TestApplyManifestDynamic_NonDryRunCreatesThenUpdates covers the real
// GET→POST create branch and, on a follow-up call, the GET→PUT update
// branch. Both are exercised end-to-end through the dynamic client
// against a fake k8s REST API.
func TestApplyManifestDynamic_NonDryRunCreatesThenUpdates(t *testing.T) {
	state := &fakeK8sState{}
	api := startFakeConfigMapAPI(t, state)
	t.Cleanup(api.Close)

	server := newHelmTestServer(t, map[string]string{"c1": api.URL})

	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: default
data:
  key: v1`

	// First call: GET returns 404 → dynamic client falls through to POST.
	results, err := server.applyManifestDynamic(context.Background(), "c1", manifest, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "created", results[0].Status, "first apply must take the Create branch")
	assert.Equal(t, "ConfigMap", results[0].Kind)
	assert.Equal(t, "demo", results[0].Name)
	assert.Equal(t, "default", results[0].Namespace)

	// Second call with mutated data: GET returns the previously-stored
	// object → dynamic client takes the PUT (update) branch, and the
	// server code copies the resourceVersion from the existing object.
	manifest2 := strings.Replace(manifest, "v1", "v2", 1)
	results, err = server.applyManifestDynamic(context.Background(), "c1", manifest2, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "updated", results[0].Status, "second apply must take the Update branch")

	// And the fake API observed the updated resourceVersion.
	state.mu.Lock()
	defer state.mu.Unlock()
	stored, ok := state.stored["demo"]
	require.True(t, ok, "fake API should have the demo ConfigMap stored")
	meta, _ := stored["metadata"].(map[string]interface{})
	assert.Equal(t, "2", meta["resourceVersion"])
}

// TestParseYAML_MalformedYAMLReturnsError exercises parseYAML's early-
// return branch where yamlToJSONBytes (k8syaml.ToJSON) rejects the input.
// The rest of the function is well-covered by existing tests via the
// happy path.
func TestParseYAML_MalformedYAMLReturnsError(t *testing.T) {
	// A YAML mapping opened but not closed is a hard parse error in
	// k8s.io/apimachinery/pkg/util/yaml.
	bad := []byte("key: {unterminated: mapping\n  another: entry")

	var out map[string]interface{}
	err := parseYAML(bad, &out)
	require.Error(t, err)
}
