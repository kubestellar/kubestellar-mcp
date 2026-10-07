// Package openshift provides shared OpenShift API coordinates and
// ClusterVersion helpers used by both the MCP upgrade tools
// (pkg/mcp/tools/upgrades) and the upgrade CLI (pkg/cmd/upgrade), so the two
// consumers don't maintain independent, driftable copies of the same GVRs
// and condition-lookup logic.
package openshift

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GVRs for OpenShift upgrade-related CRDs.
var (
	ClusterVersionGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}
	ClusterOperatorGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}
	MachineConfigPoolGVR = schema.GroupVersionResource{
		Group:    "machineconfiguration.openshift.io",
		Version:  "v1",
		Resource: "machineconfigpools",
	}
)

// FindProgressingCondition walks a ClusterVersion object's
// status.conditions and returns whether the "Progressing" condition is
// True, along with its message. Both the MCP GetUpgradeStatus tool and the
// CLI watch command need this same lookup.
func FindProgressingCondition(cv *unstructured.Unstructured) (progressing bool, message string) {
	conditions, _, _ := unstructured.NestedSlice(cv.Object, "status", "conditions")
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		if condType != "Progressing" {
			continue
		}
		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		msg, _, _ := unstructured.NestedString(condMap, "message")
		return condStatus == "True", msg
	}
	return false, ""
}
