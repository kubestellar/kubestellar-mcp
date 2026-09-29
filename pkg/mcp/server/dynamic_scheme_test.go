package server

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// dynamicScheme registers the CRD GVKs that the server-package tests still
// exercise via dynfake.NewSimpleDynamicClient (currently upgrades_test.go
// and upgrades_coverage_test.go). It used to live inline in
// tools_policy_test.go; when the policy domain moved to pkg/mcp/server/policy
// (kubestellar-mcp#1027) it was duplicated: the policy sub-package keeps its
// own copy for the Gatekeeper constraint GVKs, and this file keeps the
// server-package copy for the OpenShift GVKs the upgrades tests extend it
// with in upgradesScheme. Test-only; not compiled into the server binary.
var dynamicScheme *runtime.Scheme

func init() {
	dynamicScheme = runtime.NewScheme()
	_ = corev1.AddToScheme(dynamicScheme)

	// Register Gatekeeper constraint-template + K8sRequiredLabels constraint
	// GVKs. Kept here (in addition to pkg/mcp/server/policy) because
	// upgrades tests build upgradesScheme on top of dynamicScheme, and the
	// scheme registration is orthogonal to which package owns the handler.
	ctGVK := schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplateList"}
	dynamicScheme.AddKnownTypeWithName(ctGVK, &unstructured.UnstructuredList{})
	ctItemGVK := schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplate"}
	dynamicScheme.AddKnownTypeWithName(ctItemGVK, &unstructured.Unstructured{})

	csGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabelsList"}
	dynamicScheme.AddKnownTypeWithName(csGVK, &unstructured.UnstructuredList{})
	csItemGVK := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"}
	dynamicScheme.AddKnownTypeWithName(csItemGVK, &unstructured.Unstructured{})
}
