package gitops

import (
	"strings"
	"testing"
	"unicode"
)

// Structural invariants over the kindToResource static mapping table.
//
// kind_mapping_invariants_test.go exercises the values kindToResource
// returns for specific kinds — both the ones covered by the explicit
// mappings entry and the ones that fall through to the
// `strings.ToLower(kind) + "s"` fallback. It does NOT lock the SHAPE
// of the map itself:
//
//   * Every mapping value is a valid k8s resource name (lowercase,
//     non-empty, does not end in a double "s" — the historical bug
//     shape that motivated the fallback rewrite).
//   * Every mapping key is UpperCamelCase (Kubernetes Kinds always are;
//     a lowercase key would silently never match at callsites that pass
//     GVK.Kind verbatim, becoming dead code that appears to be a mapping).
//   * Every kind in the IsClusterScoped set that also has an entry in
//     the mappings table maps to a value ending in the correct k8s
//     plural convention (guards against a "…Class" → "…classs"
//     regression on the exact class of bug the existing
//     TestKindToResource_InvariantNoDoubleS documents).
//
// These invariants are the sort a fat-fingered edit — a kind added to
// the map with an accidental upper-case letter, an empty value, or a
// "…ss" plural — would silently pass the existing behavioral tests
// today because they only assert on the specific kinds they enumerate.

// mappedKinds mirrors the keys of the kindToResource static map in
// drift.go. Kept in sync explicitly rather than reflected out of the
// unexported map so that adding a mapping requires a deliberate change
// to this invariant list too — which is where the "no dead entries"
// check lives.
var mappedKinds = []struct {
	kind     string
	resource string
}{
	{"Deployment", "deployments"},
	{"Service", "services"},
	{"ConfigMap", "configmaps"},
	{"Secret", "secrets"},
	{"Pod", "pods"},
	{"StatefulSet", "statefulsets"},
	{"DaemonSet", "daemonsets"},
	{"ReplicaSet", "replicasets"},
	{"Job", "jobs"},
	{"CronJob", "cronjobs"},
	{"Ingress", "ingresses"},
	{"ServiceAccount", "serviceaccounts"},
	{"Role", "roles"},
	{"RoleBinding", "rolebindings"},
	{"ClusterRole", "clusterroles"},
	{"ClusterRoleBinding", "clusterrolebindings"},
	{"PersistentVolumeClaim", "persistentvolumeclaims"},
	{"PersistentVolume", "persistentvolumes"},
	{"Namespace", "namespaces"},
	{"NetworkPolicy", "networkpolicies"},
	{"HorizontalPodAutoscaler", "horizontalpodautoscalers"},
	{"StorageClass", "storageclasses"},
	{"PriorityClass", "priorityclasses"},
	{"IngressClass", "ingressclasses"},
	{"RuntimeClass", "runtimeclasses"},
	{"PodSecurityPolicy", "podsecuritypolicies"},
	{"Endpoints", "endpoints"},
}

// TestKindToResource_MappingValuesAreValidResourceNames enforces the
// structural shape of every static mapping value. Values that fail
// these checks are what "kubectl api-resources" would reject as
// nonsense.
func TestKindToResource_MappingValuesAreValidResourceNames(t *testing.T) {
	for _, tt := range mappedKinds {
		tt := tt
		t.Run(tt.kind, func(t *testing.T) {
			got := kindToResource(tt.kind)
			if got != tt.resource {
				t.Fatalf(
					"invariant list drift: kindToResource(%q) = %q, want %q — "+
						"update mappedKinds in this test to match drift.go",
					tt.kind, got, tt.resource,
				)
			}
			if got == "" {
				t.Fatalf("kindToResource(%q) = empty string", tt.kind)
			}
			if strings.HasSuffix(got, "ss") {
				t.Fatalf(
					"kindToResource(%q) = %q ends in \"ss\" — historically the "+
						"exact bug shape that motivated the static entry; a "+
						"regression here silently 404s drift lookups",
					tt.kind, got,
				)
			}
			for _, r := range got {
				if !unicode.IsLower(r) && !unicode.IsDigit(r) {
					t.Fatalf(
						"kindToResource(%q) = %q — resource names must be "+
							"all-lowercase-alnum (dynamic-client GVR lookups "+
							"are case-sensitive)",
						tt.kind, got,
					)
				}
			}
		})
	}
}

// TestKindToResource_KeysAreCapitalCamelCase locks the assumption that
// every key in the static map is a Kubernetes Kind (which is always
// UpperCamelCase). An accidental lowercase key ("deployment") would
// silently be ignored by every caller — every callsite passes the
// GVK Kind which is always UpperCamelCase — leaving the entry as dead
// code that appears to be a mapping but never fires.
func TestKindToResource_KeysAreCapitalCamelCase(t *testing.T) {
	for _, tt := range mappedKinds {
		tt := tt
		t.Run(tt.kind, func(t *testing.T) {
			if tt.kind == "" {
				t.Fatal("empty kind key is invalid")
			}
			first := []rune(tt.kind)[0]
			if !unicode.IsUpper(first) {
				t.Fatalf(
					"kind key %q must be UpperCamelCase — Kubernetes Kinds "+
						"are always UpperCamelCase and callsites pass GVK.Kind "+
						"verbatim; a lowercase key silently never matches",
					tt.kind,
				)
			}
		})
	}
}

// TestKindToResource_ClassKindsMapToClassesNotClasss adds a targeted
// regression guard for the specific bug family that
// TestKindToResource_InvariantNoDoubleS documents at the kind level:
// every "…Class" or "…Policy" kind in the static map must plural-ize
// to the correct k8s convention (…classes / …policies), never the
// naive fallback (…classs / …policys).
func TestKindToResource_ClassKindsMapToClassesNotClasss(t *testing.T) {
	for _, tt := range mappedKinds {
		tt := tt
		if !strings.HasSuffix(tt.kind, "Class") && !strings.HasSuffix(tt.kind, "Policy") {
			continue
		}
		t.Run(tt.kind, func(t *testing.T) {
			got := kindToResource(tt.kind)
			if strings.HasSuffix(tt.kind, "Class") {
				want := strings.ToLower(strings.TrimSuffix(tt.kind, "Class")) + "classes"
				if got != want {
					t.Fatalf(
						"kindToResource(%q) = %q — every …Class kind must "+
							"plural-ize to …classes (want %q). Regression on "+
							"the exact bug shape documented by "+
							"TestKindToResource_InvariantNoDoubleS.",
						tt.kind, got, want,
					)
				}
			}
			if strings.HasSuffix(tt.kind, "Policy") {
				want := strings.ToLower(strings.TrimSuffix(tt.kind, "Policy")) + "policies"
				if got != want {
					t.Fatalf(
						"kindToResource(%q) = %q — every …Policy kind must "+
							"plural-ize to …policies (want %q).",
						tt.kind, got, want,
					)
				}
			}
		})
	}
}
