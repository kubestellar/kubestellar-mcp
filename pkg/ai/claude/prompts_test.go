package claude

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptIncludesContext(t *testing.T) {
	prompt := BuildSystemPrompt(ClusterContext{
		Clusters:         []string{"alpha", "beta"},
		CurrentCluster:   "alpha",
		CurrentNamespace: "demo",
	})

	checks := []string{
		"You are kubectl-claude",
		"- Current cluster: alpha",
		"- Current namespace: demo",
		"- Available clusters: alpha, beta",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Fatalf("BuildSystemPrompt() missing %q in %q", check, prompt)
		}
	}
}

func TestBuildQueryPrompt(t *testing.T) {
	if got := BuildQueryPrompt("show pods", ""); got != "show pods" {
		t.Fatalf("BuildQueryPrompt() without context = %q", got)
	}

	withContext := BuildQueryPrompt("show pods", "pod/demo is CrashLoopBackOff")
	if !strings.Contains(withContext, "Additional context from the cluster") || !strings.Contains(withContext, "pod/demo is CrashLoopBackOff") {
		t.Fatalf("BuildQueryPrompt() missing context: %q", withContext)
	}
}

func TestFormatResourceContextAndCommonPrompts(t *testing.T) {
	formatted := FormatResourceContext("pods", []string{"pod-a", "pod-b"})
	if !strings.Contains(formatted, "Current pods state") || !strings.Contains(formatted, "pod-a") {
		t.Fatalf("FormatResourceContext() = %q", formatted)
	}
	if CommonPrompts.TroubleshootPrefix == "" || CommonPrompts.CommandPrefix == "" {
		t.Fatal("common prompt prefixes should not be empty")
	}
}
