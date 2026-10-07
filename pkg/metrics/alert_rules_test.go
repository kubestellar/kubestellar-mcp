package metrics

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// alertRulesFile mirrors the subset of the PrometheusRule CRD structure used
// by docs/alerts/mcpserver-rules.yaml.
type alertRulesFile struct {
	Spec struct {
		Groups []struct {
			Name  string `json:"name"`
			Rules []struct {
				Alert string `json:"alert"`
				Expr  string `json:"expr"`
			} `json:"rules"`
		} `json:"groups"`
	} `json:"spec"`
}

var (
	// metricNameRE pulls every metric's registered Name out of this
	// package's source, so the expected set always tracks metrics.go
	// without needing to be maintained by hand.
	metricNameRE = regexp.MustCompile(`Name:\s*"(mcpserver_[a-zA-Z0-9_]+)"`)

	// counterTotalRE pulls just the "_total" counter names, the subset of
	// metrics.go's registrations that count discrete outcomes (as opposed
	// to a duration histogram) and are therefore the ones an alert rule
	// would reasonably fire on.
	counterTotalRE = regexp.MustCompile(`Name:\s*"(mcpserver_[a-zA-Z0-9_]*_total)"`)

	// tokenRE finds every mcpserver_* identifier referenced by an alert
	// expr, including PromQL selectors and label matchers.
	tokenRE = regexp.MustCompile(`\bmcpserver_[a-zA-Z0-9_]*\b`)

	// histogramSuffixes are the series suffixes Prometheus appends to a
	// histogram's base metric name; an expr referencing "<name>_bucket"
	// is referencing the histogram defined with base name "<name>".
	histogramSuffixes = []string{"_bucket", "_sum", "_count"}
)

// metricsWithoutAlertCoverage lists mcpserver_*_total counters that are
// intentionally not yet referenced by any alert rule in
// docs/alerts/mcpserver-rules.yaml. Every entry must cite an *open* issue
// tracking its follow-up alert. TestEveryCounterHasAlertCoverage fails on
// any unlisted, unreferenced counter so a newly-added outcome metric can't
// silently ship with no alerting coverage - see kubestellar-mcp#1159, which
// found this had already happened twice.
//
// Both entries below were previously cited to #1159 and #1163
// respectively, but each of those issues was closed (#1159 by #1162/#1172,
// #1163 by a scanner sweep) without landing the real alert rule its own
// "Follow-up" section called for - the citations pointed at closed issues
// with no open tracker for the still-missing alerts. kubestellar-mcp#1178
// is the replacement open tracker for both until real alert rules land (or
// a maintainer decides, per docs/alerts/README.md, that one or both metrics
// will never get one and this entry should be documented as permanent
// rather than cite an issue at all).
var metricsWithoutAlertCoverage = map[string]string{
	"mcpserver_gitops_drift_total":    "kubestellar-mcp#1178",
	"mcpserver_ai_query_errors_total": "kubestellar-mcp#1178",
}

// readAlertRules loads and parses docs/alerts/mcpserver-rules.yaml, shared
// by both tests in this file.
func readAlertRules(t *testing.T) alertRulesFile {
	t.Helper()
	rulesPath := filepath.Join("..", "..", "docs", "alerts", "mcpserver-rules.yaml")
	raw, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("reading %s: %v", rulesPath, err)
	}

	var rules alertRulesFile
	if err := yaml.Unmarshal(raw, &rules); err != nil {
		t.Fatalf("parsing %s: %v", rulesPath, err)
	}
	return rules
}

// alertReferencedBaseMetrics returns the set of base metric names (histogram
// suffixes stripped) referenced by at least one alert expr.
func alertReferencedBaseMetrics(rules alertRulesFile) map[string]bool {
	referenced := map[string]bool{}
	for _, g := range rules.Spec.Groups {
		for _, r := range g.Rules {
			for _, tok := range tokenRE.FindAllString(r.Expr, -1) {
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

// TestAlertRulesReferenceRegisteredMetrics guards
// docs/alerts/mcpserver-rules.yaml against drifting from this package: every
// mcpserver_* token used in an alert expr must name a metric actually
// defined here (as its base name, or with a histogram's
// _bucket/_sum/_count suffix). Nothing else in CI checks this -
// dashboard-alert-lint.yml's `make alert-lint` only validates that the YAML
// parses and spec.groups is non-empty (see Makefile's alert-lint target),
// so a renamed or removed metric can silently leave a dangling alert (or a
// typo'd one can silently never fire) with no test catching it.
func TestAlertRulesReferenceRegisteredMetrics(t *testing.T) {
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

	rules := readAlertRules(t)

	seen := 0
	for _, g := range rules.Spec.Groups {
		for _, r := range g.Rules {
			for _, tok := range tokenRE.FindAllString(r.Expr, -1) {
				base := tok
				for _, suf := range histogramSuffixes {
					base = strings.TrimSuffix(base, suf)
				}
				if base == "" || base == "mcpserver_" {
					continue // bare prefix match, not a real series name
				}
				seen++
				if !defined[base] {
					t.Errorf("alert %q (group %q) expr references %q (base metric %q), which is not defined in pkg/metrics/metrics.go; update the alert or the metric", r.Alert, g.Name, tok, base)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no mcpserver_* tokens found in any alert expr - YAML parsing or regex is likely broken")
	}
}

// TestEveryCounterHasAlertCoverage guards the opposite direction of
// TestAlertRulesReferenceRegisteredMetrics: every mcpserver_*_total counter
// registered in metrics.go must be referenced by at least one alert expr in
// docs/alerts/mcpserver-rules.yaml, unless it is listed (with a tracking
// issue) in metricsWithoutAlertCoverage above. Without this, a new outcome
// counter can ship with no operator-facing alert and nothing in CI notices -
// see kubestellar-mcp#1159, which found this had already happened twice.
func TestEveryCounterHasAlertCoverage(t *testing.T) {
	src, err := os.ReadFile("metrics.go")
	if err != nil {
		t.Fatalf("reading metrics.go: %v", err)
	}
	counters := counterTotalRE.FindAllStringSubmatch(string(src), -1)
	if len(counters) == 0 {
		t.Fatal("no mcpserver_*_total counters found in metrics.go - regex is likely broken")
	}

	referenced := alertReferencedBaseMetrics(readAlertRules(t))

	for _, m := range counters {
		name := m[1]
		if referenced[name] {
			if issue, exempt := metricsWithoutAlertCoverage[name]; exempt {
				t.Errorf("%s is referenced by an alert rule now - remove its stale exemption (tracked by %s) from metricsWithoutAlertCoverage", name, issue)
			}
			continue
		}
		if _, exempt := metricsWithoutAlertCoverage[name]; exempt {
			continue
		}
		t.Errorf("%s has no alert rule in docs/alerts/mcpserver-rules.yaml and no exemption in metricsWithoutAlertCoverage; add an alert rule or an explicitly-tracked exemption", name)
	}
}
