package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// TestHandleAddLabels_FailedSummaryArm covers the failed-summary arm of
// handleAddLabels at pkg/deploy/mcp/tools_labels.go:80-87:
//
//	if result.Error != "" {
//	    labelResults = append(labelResults, LabelResult{
//	        Cluster: result.Cluster,
//	        Kind:    params.Kind,
//	        Name:    params.Name,
//	        Status:  "failed",
//	        Message: result.Error,
//	    })
//	}
//
// The existing multi-cluster tests exercise the success arm through
// TestHandleAddLabels_HappyPath_TwoClusters but never exercise the branch
// that converts a ClusterResult.Error into a LabelResult{Status: "failed"}.
// That branch is the entire way per-cluster failures are surfaced to
// callers, so it is exactly the arm you want pinned.
//
// The failure is triggered purely through the public handler by targeting
// a real cluster ("cA") plus a cluster name that is absent from the
// kubeconfig ("ghost"). The executor's GetClient rejects "ghost" and
// surfaces the "cluster not found" text as ClusterResult.Error; the
// summary loop must translate that into a failed LabelResult while still
// labeling cA successfully. Mixed success/failure in one test pins the
// full for-range loop, keeping the covered success arm covered too.
func TestHandleAddLabels_FailedSummaryArm(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"kind":"deployment","name":"demo","namespace":"apps","labels":{"env":"prod"},"clusters":["cA","ghost"]}`)
	res, err := srv.handleAddLabels(context.Background(), args)
	if err != nil {
		t.Fatalf("handleAddLabels: %v", err)
	}
	m := decodeLabelsResp(t, res)

	// One success ("cA" → labeled) + one failure ("ghost" via
	// GetClient error) → successCount stays at 1.
	if got := int(m["successCount"].(float64)); got != 1 {
		t.Fatalf("successCount = %d, want 1 (cA success, ghost failed via GetClient)", got)
	}
	if got := int(m["totalClusters"].(float64)); got != 2 {
		t.Fatalf("totalClusters = %d, want 2", got)
	}

	// results must contain exactly one Status=="failed" for ghost with a
	// non-empty Message copied from ClusterResult.Error, and one
	// Status=="labeled" for cA.
	items, ok := m["results"].([]interface{})
	if !ok {
		t.Fatalf("results not a slice: %T", m["results"])
	}
	var failed, succeeded int
	var failedCluster, failedMessage string
	var succeededCluster string
	for _, it := range items {
		r, _ := it.(map[string]interface{})
		switch r["status"] {
		case "failed":
			failed++
			failedCluster, _ = r["cluster"].(string)
			failedMessage, _ = r["message"].(string)
		case "labeled":
			succeeded++
			succeededCluster, _ = r["cluster"].(string)
		}
	}
	if failed != 1 {
		t.Fatalf("failed count = %d, want 1", failed)
	}
	if succeeded != 1 {
		t.Fatalf("labeled count = %d, want 1", succeeded)
	}
	if failedCluster != "ghost" {
		t.Fatalf("failed cluster = %q, want %q", failedCluster, "ghost")
	}
	if failedMessage == "" {
		t.Fatal("failed LabelResult must propagate ClusterResult.Error into Message")
	}
	if succeededCluster != "cA" {
		t.Fatalf("labeled cluster = %q, want %q", succeededCluster, "cA")
	}
}

// TestHandleRemoveLabels_FailedSummaryArm mirrors the assertion above for
// handleRemoveLabels at pkg/deploy/mcp/tools_labels.go:232-239. The same
// mixed-success/failure input shape exercises both the failed-summary arm
// and the success arm so the full aggregation loop stays pinned.
func TestHandleRemoveLabels_FailedSummaryArm(t *testing.T) {
	mgr, cleanup := managerWithAppsServers(t, map[string]findAppFixtures{
		"cA": {},
	})
	defer cleanup()

	srv := newServerWithManager(mgr)
	args := json.RawMessage(`{"kind":"deployment","name":"demo","namespace":"apps","labels":["env"],"clusters":["cA","ghost"]}`)
	res, err := srv.handleRemoveLabels(context.Background(), args)
	if err != nil {
		t.Fatalf("handleRemoveLabels: %v", err)
	}
	m := decodeLabelsResp(t, res)

	if got := int(m["successCount"].(float64)); got != 1 {
		t.Fatalf("successCount = %d, want 1", got)
	}
	if got := int(m["totalClusters"].(float64)); got != 2 {
		t.Fatalf("totalClusters = %d, want 2", got)
	}

	items, ok := m["results"].([]interface{})
	if !ok {
		t.Fatalf("results not a slice: %T", m["results"])
	}
	var failed int
	var failedCluster, failedMessage string
	for _, it := range items {
		r, _ := it.(map[string]interface{})
		if r["status"] == "failed" {
			failed++
			failedCluster, _ = r["cluster"].(string)
			failedMessage, _ = r["message"].(string)
		}
	}
	if failed != 1 {
		t.Fatalf("failed count = %d, want 1", failed)
	}
	if failedCluster != "ghost" {
		t.Fatalf("failed cluster = %q, want %q", failedCluster, "ghost")
	}
	if failedMessage == "" {
		t.Fatal("failed LabelResult must propagate ClusterResult.Error into Message")
	}
}
