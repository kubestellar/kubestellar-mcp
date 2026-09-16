package mcp

import (
	"context"
	"sort"
	"strings"
	"testing"
)

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
