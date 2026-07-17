package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeLabelsResp(t *testing.T, res interface{}) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func labelResultsStatuses(m map[string]interface{}) []string {
	items, _ := m["results"].([]interface{})
	out := make([]string, 0, len(items))
	for _, it := range items {
		r, _ := it.(map[string]interface{})
		s, _ := r["status"].(string)
		out = append(out, s)
	}
	return out
}

// TestHandleAddLabels_HappyPath_TwoClusters exercises the full multi-cluster
// path: request body parsing, executor fan-out, per-cluster PATCH on
// Deployments, and aggregation into the response.
func TestHandleAddLabels_HappyPath_TwoClusters(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {},
		"cB": {},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"kind":"deployment","name":"demo","namespace":"apps","labels":{"env":"prod"},"clusters":["cA","cB"]}`)
	res, err := srv.handleAddLabels(context.Background(), args)
	if err != nil {
		t.Fatalf("handleAddLabels: %v", err)
	}
	m := decodeLabelsResp(t, res)
	if got := int(m["successCount"].(float64)); got != 2 {
		t.Fatalf("successCount = %d, want 2", got)
	}
	if got := int(m["totalClusters"].(float64)); got != 2 {
		t.Fatalf("totalClusters = %d, want 2", got)
	}
	if m["dryRun"].(bool) {
		t.Fatalf("dryRun should be false")
	}
	statuses := labelResultsStatuses(m)
	if len(statuses) != 2 || statuses[0] != "labeled" || statuses[1] != "labeled" {
		t.Fatalf("statuses = %v, want [labeled labeled]", statuses)
	}
}

// TestHandleAddLabels_DryRunDiscoverClusters exercises the DiscoverClusters
// fallback when Clusters is empty, plus the dry_run branch of
// addLabelsInCluster.
func TestHandleAddLabels_DryRunDiscoverClusters(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"kind":"deployment","name":"demo","namespace":"apps","labels":{"env":"prod"},"dry_run":true}`)
	res, err := srv.handleAddLabels(context.Background(), args)
	if err != nil {
		t.Fatalf("handleAddLabels: %v", err)
	}
	m := decodeLabelsResp(t, res)
	if !m["dryRun"].(bool) {
		t.Fatalf("dryRun should be true")
	}
	if got := int(m["successCount"].(float64)); got != 1 {
		t.Fatalf("successCount = %d, want 1", got)
	}
	targets, _ := m["targetClusters"].([]interface{})
	if len(targets) != 1 || targets[0].(string) != "cA" {
		t.Fatalf("targetClusters = %v, want [cA]", targets)
	}
	statuses := labelResultsStatuses(m)
	if len(statuses) != 1 || statuses[0] != "would-label" {
		t.Fatalf("statuses = %v, want [would-label]", statuses)
	}
}

// TestHandleRemoveLabels_HappyPath_TwoClusters mirrors the add test for the
// remove path, verifying the "unlabeled" status count aggregation.
func TestHandleRemoveLabels_HappyPath_TwoClusters(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {},
		"cB": {},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"kind":"deployment","name":"demo","namespace":"apps","labels":["env","team"],"clusters":["cA","cB"]}`)
	res, err := srv.handleRemoveLabels(context.Background(), args)
	if err != nil {
		t.Fatalf("handleRemoveLabels: %v", err)
	}
	m := decodeLabelsResp(t, res)
	if got := int(m["successCount"].(float64)); got != 2 {
		t.Fatalf("successCount = %d, want 2", got)
	}
	statuses := labelResultsStatuses(m)
	if len(statuses) != 2 || statuses[0] != "unlabeled" || statuses[1] != "unlabeled" {
		t.Fatalf("statuses = %v, want [unlabeled unlabeled]", statuses)
	}
	keys, _ := m["labelKeys"].([]interface{})
	if len(keys) != 2 {
		t.Fatalf("labelKeys = %v, want 2 entries", keys)
	}
}

// TestHandleRemoveLabels_DryRunDiscoverClusters exercises DiscoverClusters
// fallback + would-unlabel branch.
func TestHandleRemoveLabels_DryRunDiscoverClusters(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"only": {},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"kind":"deployment","name":"demo","labels":["env"],"dry_run":true}`)
	res, err := srv.handleRemoveLabels(context.Background(), args)
	if err != nil {
		t.Fatalf("handleRemoveLabels: %v", err)
	}
	m := decodeLabelsResp(t, res)
	if got := int(m["successCount"].(float64)); got != 1 {
		t.Fatalf("successCount = %d, want 1", got)
	}
	statuses := labelResultsStatuses(m)
	if len(statuses) != 1 || statuses[0] != "would-unlabel" {
		t.Fatalf("statuses = %v, want [would-unlabel]", statuses)
	}
}

// startNotFoundServer returns 404 for any Deployment PATCH, exercising the
// "not found" branch of addLabelsInCluster/removeLabelsInCluster.
func startNotFoundServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","message":"deployments.apps \"demo\" not found","code":404}`))
	}))
}

func TestAddLabelsInCluster_NotFoundMapsToNotFoundStatus(t *testing.T) {
	srv := startNotFoundServer(t)
	defer srv.Close()
	s := &Server{}
	res, err := s.addLabelsInCluster(context.Background(), clientForServer(t, srv), "cA", "deployment", "demo", "apps", map[string]string{"env": "prod"}, false)
	if err != nil {
		t.Fatalf("addLabelsInCluster: %v", err)
	}
	if res.Status != "not-found" || !strings.Contains(res.Message, "not found") {
		t.Fatalf("unexpected result: %#v", res)
	}
}

func TestRemoveLabelsInCluster_NotFoundMapsToNotFoundStatus(t *testing.T) {
	srv := startNotFoundServer(t)
	defer srv.Close()
	s := &Server{}
	res, err := s.removeLabelsInCluster(context.Background(), clientForServer(t, srv), "cA", "deployment", "demo", "", []string{"env"}, false)
	if err != nil {
		t.Fatalf("removeLabelsInCluster: %v", err)
	}
	if res.Status != "not-found" || !strings.Contains(res.Message, "not found") {
		t.Fatalf("unexpected result: %#v", res)
	}
}

// startServerErrServer returns 500 for any request; exercises the generic
// "failed" branch of add/removeLabelsInCluster.
func startServerErrServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
}

func TestAddLabelsInCluster_ServerErrorMapsToFailed(t *testing.T) {
	srv := startServerErrServer(t)
	defer srv.Close()
	s := &Server{}
	res, err := s.addLabelsInCluster(context.Background(), clientForServer(t, srv), "cA", "deployment", "demo", "", map[string]string{"env": "prod"}, false)
	if err != nil {
		t.Fatalf("addLabelsInCluster: %v", err)
	}
	if res.Status != "failed" || res.Message == "" {
		t.Fatalf("unexpected result: %#v", res)
	}
}

func TestRemoveLabelsInCluster_ServerErrorMapsToFailed(t *testing.T) {
	srv := startServerErrServer(t)
	defer srv.Close()
	s := &Server{}
	res, err := s.removeLabelsInCluster(context.Background(), clientForServer(t, srv), "cA", "deployment", "demo", "", []string{"env"}, false)
	if err != nil {
		t.Fatalf("removeLabelsInCluster: %v", err)
	}
	if res.Status != "failed" || res.Message == "" {
		t.Fatalf("unexpected result: %#v", res)
	}
}

// (end)
