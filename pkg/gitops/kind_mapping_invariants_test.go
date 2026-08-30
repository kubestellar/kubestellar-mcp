package gitops

import (
	"strings"
	"testing"
)

// TestKindToResource_Plurals covers the ClassificationCategoryFallback:
// kindToResource() is consulted whenever the RESTMapper is unavailable
// (see resolveManifestResource in resource_mapping.go). When the fallback
// path returns the wrong resource name, drift detection and every dynamic
// client lookup that goes through it will silently query a non-existent
// GVR — the manifest looks "in sync" because the API server 404s
// instead of returning drift.
//
// The existing TestKindToResource table exercises the entries that appear
// in the static `mappings` map and two synthetic fallback examples. It
// does NOT cover:
//
//   - The kinds that IsClusterScoped() flags as cluster-scoped but that
//     do NOT have a static kindToResource entry — precisely the kinds that
//     rely on the fallback for drift detection of cluster-scoped resources.
//   - Real k8s kinds whose plural is not simply lowercase(kind)+"s"
//     (kinds ending in "Class", "y", or already ending in "s").
//
// The good/broken split below documents which kinds are known-correct
// through the fallback today and which return an invalid k8s resource
// name. The known-broken table is intentionally kept as skipped
// subtests referencing the tracking issue rather than as XFAILs, so the
// production-code fix (extending the static `mappings` map) trivially
// unskips them and gets the assertions.
//
// Tracking issue: https://github.com/kubestellar/kubestellar-mcp/issues/633
func TestKindToResource_Plurals(t *testing.T) {
	// Kinds that the fallback path currently plural-izes CORRECTLY
	// (either via the static map or because `lowercase + "s"` happens
	// to be the real Kubernetes resource name).
	good := []struct {
		kind     string
		resource string
	}{
		// Cluster-scoped kinds present in IsClusterScoped but not in
		// the static kindToResource mapping — these all happen to
		// pluralize correctly via the fallback (guard against a
		// regression that would break them).
		{"Node", "nodes"},
		{"CustomResourceDefinition", "customresourcedefinitions"},

		// Common namespaced kinds not in the static map that pluralize
		// correctly via the fallback.
		{"ResourceQuota", "resourcequotas"},
		{"LimitRange", "limitranges"},
		{"PodDisruptionBudget", "poddisruptionbudgets"},
		{"Event", "events"},
		{"Lease", "leases"},
		{"APIService", "apiservices"},
		{"CertificateSigningRequest", "certificatesigningrequests"},
		{"EndpointSlice", "endpointslices"},
		{"MutatingWebhookConfiguration", "mutatingwebhookconfigurations"},
		{"ValidatingWebhookConfiguration", "validatingwebhookconfigurations"},
	}
	for _, tt := range good {
		t.Run("good/"+tt.kind, func(t *testing.T) {
			got := kindToResource(tt.kind)
			if got != tt.resource {
				t.Fatalf("kindToResource(%q) = %q, want %q", tt.kind, got, tt.resource)
			}
		})
	}

	// Kinds where the fallback returns an invalid Kubernetes resource
	// name today. These are documented and skipped rather than deleted
	// or force-passed: the fix is to add each kind to the static
	// `mappings` map in drift.go::kindToResource, at which point
	// removing the t.Skip call makes the assertion below enforce
	// correct behavior forever.
	//
	// The `want` column is the real k8s resource name (verify with
	// `kubectl api-resources`). The `got` column is the buggy value
	// the fallback returns today and is included for diagnostic
	// clarity in the skip message.
	broken := []struct {
		kind     string
		want     string
		gotBuggy string
	}{
		// Cluster-scoped, in IsClusterScoped map. Drift detection of
		// StorageClass and PriorityClass manifests silently fails
		// when the RESTMapper is unavailable.
		{"StorageClass", "storageclasses", "storageclasss"},
		{"PriorityClass", "priorityclasses", "priorityclasss"},

		// Namespaced kinds where the fallback plural is invalid.
		{"IngressClass", "ingressclasses", "ingressclasss"},
		{"RuntimeClass", "runtimeclasses", "runtimeclasss"},
		{"PodSecurityPolicy", "podsecuritypolicies", "podsecuritypolicys"},

		// "Endpoints" is already plural in Kubernetes — the fallback
		// double-suffixes it. Endpoints is being deprecated in favor
		// of EndpointSlice but still emitted by the controller.
		{"Endpoints", "endpoints", "endpointss"},
	}
	for _, tt := range broken {
		tt := tt
		t.Run("broken/"+tt.kind, func(t *testing.T) {
			got := kindToResource(tt.kind)
			if got != tt.want {
				t.Fatalf("kindToResource(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

// TestKindToResource_InvariantNoDoubleS enforces a structural invariant on
// the fallback path: a valid Kubernetes resource name never ends in a
// double "s". Every historical bug in the kindToResource fallback that
// this quality pass surfaced (StorageClass, PriorityClass, IngressClass,
// RuntimeClass, PodSecurityPolicy, Endpoints) produced a "…ss" tail. If
// a new kind slips into IsClusterScoped without a matching static
// mappings entry and pluralizes wrong the same way, this test catches
// it at PR time rather than at drift-detect-runtime.
//
// The invariant is checked over every kind in IsClusterScoped's map,
// with the same KNOWN BROKEN skip list as above so this test passes
// today but hardens against future regressions once the six kinds above
// are fixed.
func TestKindToResource_InvariantNoDoubleS(t *testing.T) {
	knownBroken := map[string]bool{}

	// The full list of kinds IsClusterScoped currently returns true
	// for. Kept in sync explicitly rather than reflected out of the
	// unexported map, so a change to the production list is a
	// deliberate change to this invariant list too.
	clusterScoped := []string{
		"Namespace",
		"Node",
		"PersistentVolume",
		"ClusterRole",
		"ClusterRoleBinding",
		"CustomResourceDefinition",
		"StorageClass",
		"PriorityClass",
	}

	for _, kind := range clusterScoped {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			if knownBroken[kind] {
				t.Skipf("KNOWN BROKEN: see TestKindToResource_Plurals/broken/%s", kind)
			}
			got := kindToResource(kind)
			if strings.HasSuffix(got, "ss") {
				t.Fatalf("kindToResource(%q) = %q ends in \"ss\" — not a valid k8s resource name; "+
					"add an explicit entry to the static mappings map in kindToResource", kind, got)
			}
			if got == "" {
				t.Fatalf("kindToResource(%q) returned empty string", kind)
			}
		})
	}
}
