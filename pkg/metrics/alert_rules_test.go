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

	// tokenRE finds every mcpserver_* identifier referenced by an alert
	// expr, including PromQL selectors and label matchers.
	tokenRE = regexp.MustCompile(`\bmcpserver_[a-zA-Z0-9_]*\b`)

	// histogramSuffixes are the series suffixes Prometheus appends to a
	// histogram's base metric name; an expr referencing "<name>_bucket"
	// is referencing the histogram defined with base name "<name>".
	histogramSuffixes = []string{"_bucket", "_sum", "_count"}
)

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

	rulesPath := filepath.Join("..", "..", "docs", "alerts", "mcpserver-rules.yaml")
	raw, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("reading %s: %v", rulesPath, err)
	}

	var rules alertRulesFile
	if err := yaml.Unmarshal(raw, &rules); err != nil {
		t.Fatalf("parsing %s: %v", rulesPath, err)
	}

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
