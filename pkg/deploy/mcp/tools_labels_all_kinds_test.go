package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// patchAcceptingServer returns an httptest.Server that accepts PATCH requests
// on the seven CoreV1 and AppsV1 resource kinds routed by
// addLabelsInCluster/removeLabelsInCluster and replies with a minimal valid
// JSON body. Any non-PATCH or unrecognised path returns 404 so that a
// regression that stopped routing a kind at all would surface as a test
// failure rather than a silent success.
//
// The switch statements in tools_labels.go cover:
//   - deployment, service, configmap, pod, statefulset, daemonset,
//     namespace, node, persistentvolume, persistentvolumeclaim
//   - and short aliases: svc, cm, sts, ds, ns, pv, pvc
//
// Prior tests (tools_labels_multicluster_test.go) only exercised the
// "deployment" branch plus the sensitive-kind and dry-run early returns,
// leaving nine kinds and seven aliases unexercised.
func patchAcceptingServer(t *testing.T, recorded *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.NotFound(w, r)
			return
		}
		if recorded != nil {
			*recorded = append(*recorded, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// A minimal empty object satisfies the client's decoder for
		// Patch on any typed resource; the client's typed decoder only
		// needs kind + apiVersion when it's set, but empty {} is
		// tolerated on PATCH responses.
		_, _ = w.Write([]byte(`{}`))
	}))
}

// TestAddLabelsInCluster_AllKinds walks every kind (and alias) in the
// addLabelsInCluster switch and asserts each one dispatches a PATCH against
// the correct REST path. A regression that dropped a case, mistyped an alias,
// or routed a kind to the wrong client group would show up as either the
// wrong URL landing on the recorded server or as a "labeled" -> "failed"
// status flip.
func TestAddLabelsInCluster_AllKinds(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()

	client := clientForServer(t, srv)
	s := &Server{}

	cases := []struct {
		kind         string
		wantPathPart string
	}{
		{"deployment", "/apis/apps/v1/namespaces/apps/deployments/demo"},
		{"deployments", "/apis/apps/v1/namespaces/apps/deployments/demo"},
		{"service", "/api/v1/namespaces/apps/services/demo"},
		{"svc", "/api/v1/namespaces/apps/services/demo"},
		{"configmap", "/api/v1/namespaces/apps/configmaps/demo"},
		{"cm", "/api/v1/namespaces/apps/configmaps/demo"},
		{"pod", "/api/v1/namespaces/apps/pods/demo"},
		{"pods", "/api/v1/namespaces/apps/pods/demo"},
		{"statefulset", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"sts", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"daemonset", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"ds", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"namespace", "/api/v1/namespaces/demo"},
		{"ns", "/api/v1/namespaces/demo"},
		{"node", "/api/v1/nodes/demo"},
		{"nodes", "/api/v1/nodes/demo"},
		{"persistentvolume", "/api/v1/persistentvolumes/demo"},
		{"pv", "/api/v1/persistentvolumes/demo"},
		{"persistentvolumeclaim", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
		{"pvc", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
	}

	labels := map[string]string{"env": "prod"}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			recorded = recorded[:0]
			result, err := s.addLabelsInCluster(
				context.Background(), client, "cA",
				tc.kind, "demo", "apps", labels, false,
			)
			if err != nil {
				t.Fatalf("addLabelsInCluster(%q): unexpected error %v", tc.kind, err)
			}
			if result.Status != "labeled" {
				t.Fatalf("addLabelsInCluster(%q): status=%q msg=%q, want 'labeled'",
					tc.kind, result.Status, result.Message)
			}
			if len(recorded) != 1 {
				t.Fatalf("addLabelsInCluster(%q): expected 1 PATCH, recorded %v",
					tc.kind, recorded)
			}
			if !strings.Contains(recorded[0], tc.wantPathPart) {
				t.Errorf("addLabelsInCluster(%q): recorded path %q does not contain %q",
					tc.kind, recorded[0], tc.wantPathPart)
			}
		})
	}
}

// TestRemoveLabelsInCluster_AllKinds mirrors TestAddLabelsInCluster_AllKinds
// for the removal path. Same 10 kinds + 7 aliases; the "unlabeled" status is
// the success indicator and the recorded path proves the PATCH landed on
// the correct client group.
func TestRemoveLabelsInCluster_AllKinds(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()

	client := clientForServer(t, srv)
	s := &Server{}

	cases := []struct {
		kind         string
		wantPathPart string
	}{
		{"deployment", "/apis/apps/v1/namespaces/apps/deployments/demo"},
		{"service", "/api/v1/namespaces/apps/services/demo"},
		{"svc", "/api/v1/namespaces/apps/services/demo"},
		{"configmap", "/api/v1/namespaces/apps/configmaps/demo"},
		{"cm", "/api/v1/namespaces/apps/configmaps/demo"},
		{"pod", "/api/v1/namespaces/apps/pods/demo"},
		{"statefulset", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"sts", "/apis/apps/v1/namespaces/apps/statefulsets/demo"},
		{"daemonset", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"ds", "/apis/apps/v1/namespaces/apps/daemonsets/demo"},
		{"namespace", "/api/v1/namespaces/demo"},
		{"ns", "/api/v1/namespaces/demo"},
		{"node", "/api/v1/nodes/demo"},
		{"persistentvolume", "/api/v1/persistentvolumes/demo"},
		{"pv", "/api/v1/persistentvolumes/demo"},
		{"persistentvolumeclaim", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
		{"pvc", "/api/v1/namespaces/apps/persistentvolumeclaims/demo"},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			recorded = recorded[:0]
			result, err := s.removeLabelsInCluster(
				context.Background(), client, "cA",
				tc.kind, "demo", "apps", []string{"env"}, false,
			)
			if err != nil {
				t.Fatalf("removeLabelsInCluster(%q): unexpected error %v", tc.kind, err)
			}
			if result.Status != "unlabeled" {
				t.Fatalf("removeLabelsInCluster(%q): status=%q msg=%q, want 'unlabeled'",
					tc.kind, result.Status, result.Message)
			}
			if len(recorded) != 1 {
				t.Fatalf("removeLabelsInCluster(%q): expected 1 PATCH, recorded %v",
					tc.kind, recorded)
			}
			if !strings.Contains(recorded[0], tc.wantPathPart) {
				t.Errorf("removeLabelsInCluster(%q): recorded path %q does not contain %q",
					tc.kind, recorded[0], tc.wantPathPart)
			}
		})
	}
}

// TestAddLabelsInCluster_DefaultNamespaceFallback pins the `if ns == "" { ns
// = "default" }` branch: passing an empty namespace must route the PATCH to
// /namespaces/default/, not to a namespace-less URL.
func TestAddLabelsInCluster_DefaultNamespaceFallback(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()

	client := clientForServer(t, srv)
	s := &Server{}

	result, err := s.addLabelsInCluster(
		context.Background(), client, "cA",
		"configmap", "demo", "", map[string]string{"env": "prod"}, false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "labeled" {
		t.Fatalf("status=%q msg=%q, want 'labeled'", result.Status, result.Message)
	}
	if len(recorded) != 1 || !strings.Contains(recorded[0], "/api/v1/namespaces/default/configmaps/demo") {
		t.Errorf("expected PATCH to namespaces/default/configmaps/demo, got %v", recorded)
	}
}

// TestRemoveLabelsInCluster_DefaultNamespaceFallback mirrors the above for
// the removal path.
func TestRemoveLabelsInCluster_DefaultNamespaceFallback(t *testing.T) {
	var recorded []string
	srv := patchAcceptingServer(t, &recorded)
	defer srv.Close()

	client := clientForServer(t, srv)
	s := &Server{}

	result, err := s.removeLabelsInCluster(
		context.Background(), client, "cA",
		"configmap", "demo", "", []string{"env"}, false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "unlabeled" {
		t.Fatalf("status=%q msg=%q, want 'unlabeled'", result.Status, result.Message)
	}
	if len(recorded) != 1 || !strings.Contains(recorded[0], "/api/v1/namespaces/default/configmaps/demo") {
		t.Errorf("expected PATCH to namespaces/default/configmaps/demo, got %v", recorded)
	}
}

// TestAddLabelsInCluster_UnsupportedKind pins the default branch of the
// switch. Prior tests indirectly hit it via the top-level handler, but the
// direct method-level path was uncovered.
func TestAddLabelsInCluster_UnsupportedKind(t *testing.T) {
	srv := patchAcceptingServer(t, nil)
	defer srv.Close()

	client := clientForServer(t, srv)
	s := &Server{}

	result, err := s.addLabelsInCluster(
		context.Background(), client, "cA",
		"ingress", "demo", "apps", map[string]string{"env": "prod"}, false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status=%q, want 'failed'", result.Status)
	}
	if !strings.Contains(result.Message, "Unsupported resource kind: ingress") {
		t.Errorf("message=%q, want 'Unsupported resource kind: ingress'", result.Message)
	}
}

// TestRemoveLabelsInCluster_UnsupportedKind mirrors the above.
func TestRemoveLabelsInCluster_UnsupportedKind(t *testing.T) {
	srv := patchAcceptingServer(t, nil)
	defer srv.Close()

	client := clientForServer(t, srv)
	s := &Server{}

	result, err := s.removeLabelsInCluster(
		context.Background(), client, "cA",
		"ingress", "demo", "apps", []string{"env"}, false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status=%q, want 'failed'", result.Status)
	}
	if !strings.Contains(result.Message, "Unsupported resource kind: ingress") {
		t.Errorf("message=%q, want 'Unsupported resource kind: ingress'", result.Message)
	}
}

// TestBuildLabelPatch_RemoveEncodesNullValues asserts the removal branch of
// buildLabelPatch: every provided key must marshal to JSON null so a
// strategic-merge patch drops the label, not sets it to an empty string.
// The existing TestBuildLabelPatch tests the add branch but not the null-
// encoded remove semantic in isolation.
func TestBuildLabelPatch_RemoveEncodesNullValues(t *testing.T) {
	patch := buildLabelPatch(map[string]string{"env": "", "tier": ""}, true)

	var decoded map[string]interface{}
	if err := json.Unmarshal(patch, &decoded); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	meta, _ := decoded["metadata"].(map[string]interface{})
	labels, _ := meta["labels"].(map[string]interface{})
	if len(labels) != 2 {
		t.Fatalf("expected 2 label entries, got %d: %v", len(labels), labels)
	}
	for k, v := range labels {
		if v != nil {
			t.Errorf("removal patch label %q = %v (%T), want nil", k, v, v)
		}
	}
}
