package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dashboardFile mirrors the subset of docs/dashboards/mcpserver-overview.json
// structure needed to walk every panel's PromQL targets.
type dashboardFile struct {
	Panels []struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
}

// metricsWithoutDashboardPanel lists registered mcpserver_* metrics that are
// intentionally not plotted on docs/dashboards/mcpserver-overview.json. Keep
// this empty in the common case; an entry here must explain why the metric
// has no panel, mirroring metricsWithoutAlertCoverage's exemption pattern
// above so a silently-unplotted metric doesn't hide behind an unexplained
// gap.
var metricsWithoutDashboardPanel = map[string]string{}

// readDashboard loads and parses docs/dashboards/mcpserver-overview.json.
func readDashboard(t *testing.T) dashboardFile {
	t.Helper()
	dashboardPath := filepath.Join("..", "..", "docs", "dashboards", "mcpserver-overview.json")
	raw, err := os.ReadFile(dashboardPath)
	if err != nil {
		t.Fatalf("reading %s: %v", dashboardPath, err)
	}

	var dashboard dashboardFile
	if err := json.Unmarshal(raw, &dashboard); err != nil {
		t.Fatalf("parsing %s: %v", dashboardPath, err)
	}
	return dashboard
}

// dashboardReferencedBaseMetrics returns the set of base metric names
// (histogram _bucket/_sum/_count suffixes stripped) referenced by at least
// one panel target expr, mirroring alertReferencedBaseMetrics above.
func dashboardReferencedBaseMetrics(dashboard dashboardFile) map[string]bool {
	referenced := map[string]bool{}
	for _, p := range dashboard.Panels {
		for _, target := range p.Targets {
			for _, tok := range tokenRE.FindAllString(target.Expr, -1) {
				base := tok
				for _, suf := range histogramSuffixes {
					base = strings.TrimSuffix(base, suf)
				}
				if base == "" || base == "mcpserver_" {
					continue // bare prefix match, not a real series name
				}
				referenced[base] = true
			}
		}
	}
	return referenced
}

// TestDashboardReferencesRegisteredMetrics guards
// docs/dashboards/mcpserver-overview.json against drifting from this
// package: every mcpserver_* token used in a panel target expr must name a
// metric actually defined here (as its base name, or with a histogram's
// _bucket/_sum/_count suffix). This mirrors
// TestAlertRulesReferenceRegisteredMetrics - without it, a renamed or
// removed metric could silently leave a dangling dashboard panel (or a
// typo'd one could silently never render) with no test catching it, and
// make dashboard-lint.yml's syntax-only `make dashboard-lint` check
// insufficient on its own.
func TestDashboardReferencesRegisteredMetrics(t *testing.T) {
	src, err := os.ReadFile("metrics.go")
	if err != nil {
		t.Fatalf("reading metrics.go: %v", err)
	}
	defined := map[string]bool{}
	for _, m := range metricNameRE.FindAllStringSubmatch(string(src), -1) {
		defined[m[1]] = true
	}
	if len(defined) == 0 {
		t.Fatal("no mcpserver_* metric definitions found in metrics.go - regex is likely broken")
	}

	dashboard := readDashboard(t)

	seen := 0
	for _, p := range dashboard.Panels {
		for _, target := range p.Targets {
			for _, tok := range tokenRE.FindAllString(target.Expr, -1) {
				base := tok
				for _, suf := range histogramSuffixes {
					base = strings.TrimSuffix(base, suf)
				}
				if base == "" || base == "mcpserver_" {
					continue // bare prefix match, not a real series name
				}
				seen++
				if !defined[base] {
					t.Errorf("panel %q expr references %q (base metric %q), which is not defined in pkg/metrics/metrics.go; update the panel or the metric", p.Title, tok, base)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no mcpserver_* tokens found in any panel target expr - JSON parsing or regex is likely broken")
	}
}

// TestEveryMetricHasDashboardPanel guards the opposite direction of
// TestDashboardReferencesRegisteredMetrics: every mcpserver_* metric
// registered in metrics.go must be referenced by at least one panel target
// expr in docs/dashboards/mcpserver-overview.json, unless it is listed (with
// a rationale) in metricsWithoutDashboardPanel above. Without this, a new
// metric can ship with no operator-facing visualization and nothing in CI
// notices, mirroring TestEveryCounterHasAlertCoverage's rationale for
// alerts.
func TestEveryMetricHasDashboardPanel(t *testing.T) {
	src, err := os.ReadFile("metrics.go")
	if err != nil {
		t.Fatalf("reading metrics.go: %v", err)
	}
	metricNames := metricNameRE.FindAllStringSubmatch(string(src), -1)
	if len(metricNames) == 0 {
		t.Fatal("no mcpserver_* metric definitions found in metrics.go - regex is likely broken")
	}

	referenced := dashboardReferencedBaseMetrics(readDashboard(t))

	for _, m := range metricNames {
		name := m[1]
		if referenced[name] {
			if reason, exempt := metricsWithoutDashboardPanel[name]; exempt {
				t.Errorf("%s is referenced by a dashboard panel now - remove its stale exemption (%s) from metricsWithoutDashboardPanel", name, reason)
			}
			continue
		}
		if _, exempt := metricsWithoutDashboardPanel[name]; exempt {
			continue
		}
		t.Errorf("%s has no panel in docs/dashboards/mcpserver-overview.json and no exemption in metricsWithoutDashboardPanel; add a panel or an explicitly-tracked exemption", name)
	}
}
