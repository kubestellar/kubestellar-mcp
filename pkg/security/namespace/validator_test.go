package namespace

import (
	"strings"
	"testing"
)

func TestValidateNamespace_Empty(t *testing.T) {
	if err := ValidateNamespace(""); err == nil {
		t.Fatalf("expected error for empty namespace, got nil")
	}
}

func TestValidateNamespace_Overlength(t *testing.T) {
	if err := ValidateNamespace(strings.Repeat("a", 64)); err == nil {
		t.Fatalf("expected error for 64-char namespace, got nil")
	}
}

func TestValidateNamespace_InvalidSyntax(t *testing.T) {
	for _, ns := range []string{"UPPER", "-leading", "trailing-", "with_underscore", "sp ace"} {
		if err := ValidateNamespace(ns); err == nil {
			t.Errorf("expected error for %q, got nil", ns)
		}
	}
}

func TestValidateNamespace_BlockedExact(t *testing.T) {
	for _, ns := range []string{"kube-system", "kube-public", "kube-node-lease", "gatekeeper-system", "openshift"} {
		err := ValidateNamespace(ns)
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("expected 'not allowed' error for %q, got %v", ns, err)
		}
	}
}

func TestValidateNamespace_OpenshiftPrefix(t *testing.T) {
	for _, ns := range []string{"openshift-operators", "openshift-monitoring", "openshift-foo"} {
		err := ValidateNamespace(ns)
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("expected 'not allowed' error for %q, got %v", ns, err)
		}
	}
}

func TestValidateNamespace_Allowed(t *testing.T) {
	for _, ns := range []string{"default", "my-app", "team-alpha", "ns1"} {
		if err := ValidateNamespace(ns); err != nil {
			t.Errorf("unexpected error for %q: %v", ns, err)
		}
	}
}
