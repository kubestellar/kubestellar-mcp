package helm

import (
	"regexp"
	"strings"
	"testing"
)

// FuzzValidateHelmIdentifier enforces the validateHelmIdentifier contract
// as a property: for any (kind, value) input the accept/reject decision must
// match the documented rules. validateHelmIdentifier gates Helm release
// names, namespaces, and kube-context names — every one of which is
// AI-provided MCP tool input eventually spliced into a helm argv element,
// so flag-injection or invalid-DNS-label bypasses are security-relevant
// (see kubestellar-mcp#269).
//
// Companion to FuzzValidateRepoURL / FuzzValidateBranchName in pkg/gitops
// and FuzzValidateNamespace in pkg/security/namespace. This target must be
// added to the .github/workflows/fuzz.yml matrix so it is exercised by the
// weekly Fuzz workflow.
func FuzzValidateHelmIdentifier(f *testing.F) {
	seeds := []string{
		"",
		"a",
		"1",
		"my-release",
		"release.name.with.dots",
		"kube-system",
		"-flag",
		"--flag=value",
		"UPPER",
		"trailing-",
		"-leading",
		"with_underscore",
		"sp ace",
		"a/b",
		strings.Repeat("a", 253),
		"日本語",
		"\x00",
	}
	for _, s := range seeds {
		f.Add("release", s)
		f.Add("namespace", s)
	}

	// Independent model of the documented contract. Divergence between this
	// and the implementation is what the fuzzer is looking for.
	labelRe := regexp.MustCompile(`^[a-z0-9][a-z0-9\-\.]*[a-z0-9]$|^[a-z0-9]$`)

	f.Fuzz(func(t *testing.T, kind, value string) {
		err := validateHelmIdentifier(kind, value)

		// Empty is explicitly accepted (upstream required-field check owns it).
		if value == "" {
			if err != nil {
				t.Fatalf("validateHelmIdentifier(%q, %q) rejected empty value: %v", kind, value, err)
			}
			return
		}

		flagInjection := strings.HasPrefix(value, "-")
		badPattern := !labelRe.MatchString(value)
		shouldReject := flagInjection || badPattern

		if shouldReject && err == nil {
			t.Fatalf("validateHelmIdentifier(%q, %q) accepted a value that should be rejected (flagInjection=%v badPattern=%v)",
				kind, value, flagInjection, badPattern)
		}
		if !shouldReject && err != nil {
			t.Fatalf("validateHelmIdentifier(%q, %q) rejected a value the contract allows: %v", kind, value, err)
		}

		// Invariant: any accepted value is a legal helm argv element
		// (no leading '-', matches the DNS-ish label pattern).
		if err == nil {
			if strings.HasPrefix(value, "-") {
				t.Fatalf("validateHelmIdentifier(%q, %q) accepted a leading '-' value (flag injection)", kind, value)
			}
			if !labelRe.MatchString(value) {
				t.Fatalf("validateHelmIdentifier(%q, %q) accepted a value that does not match the identifier pattern", kind, value)
			}
		}
	})
}

// FuzzValidateHelmSetKey enforces the validateHelmSetKey contract as a
// property. --set keys are attacker-controlled input (AI-provided MCP tool
// arguments) that get concatenated into a comma-separated --set argument;
// commas, braces, backticks, spaces, or a leading '-' let an attacker
// inject extra key=value pairs or CLI flags (kubestellar-mcp#288).
func FuzzValidateHelmSetKey(f *testing.F) {
	seeds := []string{
		"",
		"image.tag",
		"replicas",
		"foo[0].bar",
		"foo,bar",
		"foo{bar}",
		"foo`bar`",
		"foo bar",
		"-flag",
		"a.b.c",
		"日本語",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	forbidden := ",{}` "

	f.Fuzz(func(t *testing.T, key string) {
		err := validateHelmSetKey(key)

		flagInjection := strings.HasPrefix(key, "-")
		hasForbidden := strings.ContainsAny(key, forbidden)
		shouldReject := flagInjection || hasForbidden

		if shouldReject && err == nil {
			t.Fatalf("validateHelmSetKey(%q) accepted a value that should be rejected (flagInjection=%v hasForbidden=%v)",
				key, flagInjection, hasForbidden)
		}
		if !shouldReject && err != nil {
			t.Fatalf("validateHelmSetKey(%q) rejected a value the contract allows: %v", key, err)
		}

		if err == nil {
			if strings.HasPrefix(key, "-") {
				t.Fatalf("validateHelmSetKey(%q) accepted a leading '-' key (flag injection)", key)
			}
			if strings.ContainsAny(key, forbidden) {
				t.Fatalf("validateHelmSetKey(%q) accepted a key containing a forbidden character", key)
			}
		}
	})
}

// FuzzValidateHelmSetValue enforces the validateHelmSetValue contract as a
// property. --set values are AI-provided and reach helm's --set parser
// unquoted; a comma splits them into two key=value pairs, letting an
// attacker override any other chart value (kubestellar-mcp#288).
func FuzzValidateHelmSetValue(f *testing.F) {
	seeds := []string{
		"",
		"latest",
		"v1.2.3",
		"foo,bar",
		"foo{bar}",
		"foo[0]",
		"foo`bar`",
		"日本語",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		err := validateHelmSetValue(value)

		hasForbidden := strings.ContainsAny(value, helmSetSpecialChars)
		shouldReject := hasForbidden

		if shouldReject && err == nil {
			t.Fatalf("validateHelmSetValue(%q) accepted a value containing a Helm structural character", value)
		}
		if !shouldReject && err != nil {
			t.Fatalf("validateHelmSetValue(%q) rejected a value the contract allows: %v", value, err)
		}

		if err == nil && strings.ContainsAny(value, helmSetSpecialChars) {
			t.Fatalf("validateHelmSetValue(%q) accepted a value containing a forbidden character", value)
		}
	})
}
