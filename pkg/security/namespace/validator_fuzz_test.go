package namespace

import (
	"regexp"
	"strings"
	"testing"
)

// FuzzValidateNamespace enforces the ValidateNamespace contract as a
// property under fuzzing: for any string input, the observed accept/reject
// decision must be consistent with the documented rules. Because
// ValidateNamespace is a security predicate on AI-provided MCP tool input
// (see pkg/security/namespace/validator.go), property-based fuzzing catches
// classes of bypass that fixed table-driven tests cannot.
//
// Companion to FuzzValidateRepoURL / FuzzValidateBranchName in pkg/gitops.
// Add a matching matrix entry in .github/workflows/fuzz.yml so this target
// is exercised by the weekly Fuzz workflow (kubestellar/kubestellar-mcp
// tracking issue).
func FuzzValidateNamespace(f *testing.F) {
	seeds := []string{
		"",
		"default",
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"gatekeeper-system",
		"openshift",
		"openshift-operators",
		"openshift-monitoring",
		"my-app",
		"team-alpha",
		"ns1",
		"a",
		"1",
		"UPPER",
		"-leading",
		"trailing-",
		"with_underscore",
		"sp ace",
		"a.b",
		"a/b",
		strings.Repeat("a", 63),
		strings.Repeat("a", 64),
		"\x00",
		"日本語",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	labelRe := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	blocked := map[string]bool{
		"kube-system":       true,
		"kube-public":       true,
		"kube-node-lease":   true,
		"gatekeeper-system": true,
		"openshift":         true,
	}

	f.Fuzz(func(t *testing.T, ns string) {
		err := ValidateNamespace(ns)

		// Model: reject iff any rule fires. Compute independently so the
		// fuzzer catches divergence between the documented contract and
		// the implementation (e.g. a future refactor that stops enforcing
		// length before regex, or that loosens the openshift- prefix rule).
		empty := ns == ""
		overlength := len(ns) > 63
		badSyntax := !empty && !overlength && !labelRe.MatchString(ns)
		isBlockedExact := !empty && !overlength && !badSyntax && blocked[ns]
		isOpenshiftPrefix := !empty && !overlength && !badSyntax && !isBlockedExact && strings.HasPrefix(ns, "openshift-")

		shouldReject := empty || overlength || badSyntax || isBlockedExact || isOpenshiftPrefix

		if shouldReject && err == nil {
			t.Fatalf("ValidateNamespace(%q) accepted a value that should be rejected (empty=%v overlength=%v badSyntax=%v blocked=%v openshiftPrefix=%v)",
				ns, empty, overlength, badSyntax, isBlockedExact, isOpenshiftPrefix)
		}
		if !shouldReject && err != nil {
			t.Fatalf("ValidateNamespace(%q) rejected a value the contract allows: %v", ns, err)
		}

		// Additional invariant: any accepted namespace must be a valid
		// RFC 1123 DNS label (used verbatim in kubectl calls downstream).
		if err == nil {
			if !labelRe.MatchString(ns) {
				t.Fatalf("ValidateNamespace(%q) accepted a value that is not a valid DNS label", ns)
			}
			if len(ns) == 0 || len(ns) > 63 {
				t.Fatalf("ValidateNamespace(%q) accepted a value of length %d (must be 1..63)", ns, len(ns))
			}
			if strings.HasPrefix(ns, "openshift-") {
				t.Fatalf("ValidateNamespace(%q) accepted an openshift- prefixed namespace", ns)
			}
			if blocked[ns] {
				t.Fatalf("ValidateNamespace(%q) accepted a blocked system namespace", ns)
			}
		}
	})
}
