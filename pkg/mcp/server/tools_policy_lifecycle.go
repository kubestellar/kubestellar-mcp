package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func toolInstallOwnershipPolicy(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	// Parse parameters
	labels := []string{"owner", "team"}
	if v, ok := args["labels"].([]interface{}); ok && len(v) > 0 {
		labels = make([]string, len(v))
		for i, l := range v {
			labels[i], _ = l.(string)
		}
	}

	excludeNamespaces := []string{"kube-system", "kube-public", "kube-node-lease", "gatekeeper-system"}
	if v, ok := args["exclude_namespaces"].([]interface{}); ok && len(v) > 0 {
		excludeNamespaces = make([]string, len(v))
		for i, ns := range v {
			excludeNamespaces[i], _ = ns.(string)
		}
	}

	// Add openshift namespaces if on OpenShift
	client, err := d.GetClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	// Check if this is OpenShift
	_, err = client.CoreV1().Namespaces().Get(ctx, "openshift", metav1.GetOptions{})
	isOpenShift := err == nil
	if isOpenShift {
		openshiftExcludes := []string{"openshift", "openshift-apiserver", "openshift-authentication",
			"openshift-cluster-samples-operator", "openshift-cluster-storage-operator",
			"openshift-config", "openshift-config-managed", "openshift-console",
			"openshift-controller-manager", "openshift-dns", "openshift-etcd",
			"openshift-image-registry", "openshift-infra", "openshift-ingress",
			"openshift-ingress-canary", "openshift-ingress-operator", "openshift-kube-apiserver",
			"openshift-kube-controller-manager", "openshift-kube-scheduler",
			"openshift-machine-api", "openshift-machine-config-operator",
			"openshift-marketplace", "openshift-monitoring", "openshift-multus",
			"openshift-network-diagnostics", "openshift-network-operator",
			"openshift-node", "openshift-oauth-apiserver", "openshift-operator-lifecycle-manager",
			"openshift-operators", "openshift-ovn-kubernetes", "openshift-sdn",
			"openshift-service-ca", "openshift-service-ca-operator"}
		excludeNamespaces = append(excludeNamespaces, openshiftExcludes...)
	}

	mode := "dryrun"
	if v, ok := args["mode"].(string); ok && v != "" {
		mode = v
	}

	dynClient, err := d.GetDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create dynamic client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Installing Ownership Policy\n\n")

	// Create ConstraintTemplate
	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	constraintTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": constraintTemplateAPIVersion,
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": ownershipTemplateName,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "kubectl-claude",
				},
			},
			"spec": map[string]interface{}{
				"crd": map[string]interface{}{
					"spec": map[string]interface{}{
						"names": map[string]interface{}{
							"kind": "K8sRequiredLabels",
						},
						"validation": map[string]interface{}{
							"openAPIV3Schema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"labels": map[string]interface{}{
										"type":        "array",
										"description": "List of required labels",
										"items": map[string]interface{}{
											"type": "string",
										},
									},
								},
							},
						},
					},
				},
				"targets": []interface{}{
					map[string]interface{}{
						"target": "admission.k8s.gatekeeper.sh",
						"rego": `package k8srequiredlabels

violation[{"msg": msg, "details": {"missing_labels": missing}}] {
  provided := {label | input.review.object.metadata.labels[label]}
  required := {label | label := input.parameters.labels[_]}
  missing := required - provided
  count(missing) > 0
  msg := sprintf("Resource %v/%v is missing required labels: %v", [input.review.object.kind, input.review.object.metadata.name, missing])
}`,
					},
				},
			},
		},
	}

	_, err = dynClient.Resource(ctGVR).Create(ctx, constraintTemplate, metav1.CreateOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			sb.WriteString("**ConstraintTemplate:** Already exists (updating...)\n")
			_, err = dynClient.Resource(ctGVR).Update(ctx, constraintTemplate, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Sprintf("Failed to update ConstraintTemplate: %v", err), true
			}
			sb.WriteString("**ConstraintTemplate:** Updated ✓\n")
		} else {
			return fmt.Sprintf("Failed to create ConstraintTemplate: %v", err), true
		}
	} else {
		sb.WriteString("**ConstraintTemplate:** Created ✓\n")
	}

	// Wait a moment for the CRD to be available
	time.Sleep(2 * time.Second)

	// Create Constraint
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	// Build match kinds
	matchKinds := []interface{}{
		map[string]interface{}{
			"apiGroups": []interface{}{"apps"},
			"kinds":     []interface{}{"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"},
		},
		map[string]interface{}{
			"apiGroups": []interface{}{""},
			"kinds":     []interface{}{"Pod", "Service", "ConfigMap", "Secret"},
		},
		map[string]interface{}{
			"apiGroups": []interface{}{"batch"},
			"kinds":     []interface{}{"Job", "CronJob"},
		},
	}

	excludeNSInterface := make([]interface{}, len(excludeNamespaces))
	for i, ns := range excludeNamespaces {
		excludeNSInterface[i] = ns
	}

	labelsInterface := make([]interface{}, len(labels))
	for i, l := range labels {
		labelsInterface[i] = l
	}

	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": constraintAPIVersion,
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": ownershipConstraintName,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "kubectl-claude",
				},
			},
			"spec": map[string]interface{}{
				"enforcementAction": mode,
				"match": map[string]interface{}{
					"kinds":              matchKinds,
					"excludedNamespaces": excludeNSInterface,
				},
				"parameters": map[string]interface{}{
					"labels": labelsInterface,
				},
			},
		},
	}

	_, err = dynClient.Resource(constraintGVR).Create(ctx, constraint, metav1.CreateOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			sb.WriteString("**Constraint:** Already exists (updating...)\n")
			// Get existing to preserve resource version
			existing, getErr := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Sprintf("Failed to get existing constraint: %v", getErr), true
			}
			constraint.SetResourceVersion(existing.GetResourceVersion())
			_, err = dynClient.Resource(constraintGVR).Update(ctx, constraint, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Sprintf("Failed to update Constraint: %v", err), true
			}
			sb.WriteString("**Constraint:** Updated ✓\n")
		} else {
			return fmt.Sprintf("Failed to create Constraint: %v", err), true
		}
	} else {
		sb.WriteString("**Constraint:** Created ✓\n")
	}

	_, _ = fmt.Fprintf(&sb, "\n**Mode:** %s\n", mode)
	_, _ = fmt.Fprintf(&sb, "**Required Labels:** %s\n", strings.Join(labels, ", "))
	_, _ = fmt.Fprintf(&sb, "**Excluded Namespaces:** %d namespaces\n", len(excludeNamespaces))

	sb.WriteString("\n## Next Steps\n\n")
	switch mode {
	case "dryrun":
		sb.WriteString("The policy is in **dryrun** mode. Violations are logged but resources are NOT blocked.\n\n")
		sb.WriteString("1. Use `list_ownership_violations` to see current violations\n")
		sb.WriteString("2. Fix violations by adding required labels to resources\n")
		sb.WriteString("3. Use `set_ownership_policy_mode` with mode=`warn` or `enforce` when ready\n")
	case "warn":
		sb.WriteString("The policy is in **warn** mode. Users will see warnings but resources are NOT blocked.\n\n")
		sb.WriteString("1. Use `list_ownership_violations` to see current violations\n")
		sb.WriteString("2. Use `set_ownership_policy_mode` with mode=`enforce` to start blocking\n")
	default:
		sb.WriteString("The policy is in **enforce** mode. Resources without required labels will be **BLOCKED**.\n\n")
		sb.WriteString("⚠️ Users must add these labels to all new resources:\n")
		for _, l := range labels {
			_, _ = fmt.Fprintf(&sb, "- `%s`\n", l)
		}
	}

	return sb.String(), false
}

func toolSetOwnershipPolicyMode(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	mode, _ := args["mode"].(string)

	if mode == "" {
		return "mode is required (dryrun, warn, or enforce)", true
	}

	if mode != "dryrun" && mode != "warn" && mode != "enforce" {
		return "mode must be one of: dryrun, warn, enforce", true
	}

	dynClient, err := d.GetDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	// Get existing constraint
	constraint, err := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
	if err != nil {
		return "Ownership policy not installed. Use `install_ownership_policy` first.", false
	}

	// Get current mode
	currentMode, _, _ := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
	if currentMode == "" {
		currentMode = "deny"
	}

	if currentMode == mode {
		return fmt.Sprintf("Policy is already in `%s` mode.", mode), false
	}

	// Update mode
	err = unstructured.SetNestedField(constraint.Object, mode, "spec", "enforcementAction")
	if err != nil {
		return fmt.Sprintf("Failed to set mode: %v", err), true
	}

	_, err = dynClient.Resource(constraintGVR).Update(ctx, constraint, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to update constraint: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Ownership Policy Mode Updated\n\n")
	_, _ = fmt.Fprintf(&sb, "**Previous Mode:** %s\n", currentMode)
	_, _ = fmt.Fprintf(&sb, "**New Mode:** %s\n\n", mode)

	switch mode {
	case "dryrun":
		sb.WriteString("Violations are now logged but resources are **NOT blocked**.\n")
	case "warn":
		sb.WriteString("Users will see warnings but resources are **NOT blocked**.\n")
	case "enforce":
		sb.WriteString("⚠️ Resources without required labels will now be **BLOCKED**.\n")
	}

	return sb.String(), false
}

func toolUninstallOwnershipPolicy(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	dynClient, err := d.GetDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Uninstalling Ownership Policy\n\n")

	// Delete Constraint first
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	err = dynClient.Resource(constraintGVR).Delete(ctx, ownershipConstraintName, metav1.DeleteOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			sb.WriteString("**Constraint:** Not found (already deleted)\n")
		} else {
			return fmt.Sprintf("Failed to delete constraint: %v", err), true
		}
	} else {
		sb.WriteString("**Constraint:** Deleted ✓\n")
	}

	// Delete ConstraintTemplate
	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	err = dynClient.Resource(ctGVR).Delete(ctx, ownershipTemplateName, metav1.DeleteOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			sb.WriteString("**ConstraintTemplate:** Not found (already deleted)\n")
		} else {
			return fmt.Sprintf("Failed to delete ConstraintTemplate: %v", err), true
		}
	} else {
		sb.WriteString("**ConstraintTemplate:** Deleted ✓\n")
	}

	sb.WriteString("\nOwnership policy has been removed. Resources will no longer be checked for ownership labels.\n")

	return sb.String(), false
}
