package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzSanitizeControlChars enforces the SanitizeControlChars contract as a
// property under fuzzing: for any string input, the result must be safe to
// splice into a tool response, log line, or AI prompt (see doc comment on
// SanitizeControlChars in pkg/security/sanitize/sanitize.go). Property-based
// fuzzing catches classes of contract violation — e.g. an encoding bug
// introduced by a future refactor of the truncation step — that fixed
// table-driven tests in sanitize_test.go do not exercise.
//
// Companion to FuzzValidateRepoURL / FuzzValidateBranchName in pkg/gitops,
// FuzzValidateNamespace in pkg/security/namespace, and the
// FuzzValidateHelm* targets in pkg/deploy/mcp/helm. Add a matching matrix
// entry in .github/workflows/fuzz.yml so this target is exercised by the
// weekly Fuzz workflow (tracked in kubestellar-mcp#999, which already
// covers wiring existing unmatrixed targets into that schedule).
func FuzzSanitizeControlChars(f *testing.F) {
	seeds := []string{
		"",
		"production-cluster",
		"cluster\nwith\r\ncontrol\tchars",
		"multiple   spaces    collapsed",
		strings.Repeat("a", 199),
		strings.Repeat("a", 200),
		strings.Repeat("a", 201),
		strings.Repeat("a", 300),
		"\x00\x01\x02 leading control bytes",
		"café",
		"日本語",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := SanitizeControlChars(s)

		// Contract: never contains the control characters the doc comment
		// promises to strip.
		if strings.ContainsAny(got, "\n\r\t") {
			t.Fatalf("SanitizeControlChars(%q) = %q, still contains a newline/CR/tab", s, got)
		}
		for _, r := range got {
			if r < 32 {
				t.Fatalf("SanitizeControlChars(%q) = %q, still contains control byte %U", s, got, r)
			}
		}

		// Contract: no run of 2+ spaces, and no leading/trailing space
		// (strings.Join(strings.Fields(...), " ") collapses whitespace).
		if strings.Contains(got, "  ") {
			t.Fatalf("SanitizeControlChars(%q) = %q, contains collapsed-whitespace violation (double space)", s, got)
		}
		if strings.HasPrefix(got, " ") || strings.HasSuffix(got, " ") {
			t.Fatalf("SanitizeControlChars(%q) = %q, has leading/trailing space", s, got)
		}

		// Contract: bounded length so a caller can't be made to log or
		// prompt-inject an unbounded string (200 chars + optional "...").
		if len(got) > 203 {
			t.Fatalf("SanitizeControlChars(%q) = %q, exceeds the documented 200-char (+ \"...\") bound: len=%d", s, got, len(got))
		}

		// Contract (implicit in the doc comment: "safe to splice into a
		// tool response, log line, or AI prompt"): the result must be
		// valid UTF-8. The truncation step below trims by byte index,
		// which can split a multi-byte rune when the input crosses the
		// 200-byte mark — this invariant is what would catch that class
		// of bug if a fuzzing run ever lands on such an input.
		if !utf8.ValidString(got) {
			t.Fatalf("SanitizeControlChars(%q) = %q, output is not valid UTF-8 (likely rune split at the 200-byte truncation boundary)", s, got)
		}
	})
}
