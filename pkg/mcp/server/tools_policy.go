package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (s *Server) toolCheckGatekeeper(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# OPA Gatekeeper Status\n\n")

	// Check if gatekeeper-system namespace exists
	_, err = client.CoreV1().Namespaces().Get(ctx, gatekeeperNamespace, metav1.GetOptions{})
	if err != nil {
		sb.WriteString("**Status:** Not Installed\n\n")
		sb.WriteString("Gatekeeper namespace `gatekeeper-system` not found.\n\n")
		sb.WriteString("## Installation\n\n")
		sb.WriteString("To install Gatekeeper:\n")
		sb.WriteString("```bash\n")
		sb.WriteString("kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml\n")
		sb.WriteString("```\n\n")
		sb.WriteString("Or on OpenShift, install the Gatekeeper Operator from OperatorHub.\n")
		return sb.String(), false
	}

	// Check pods in gatekeeper-system
	pods, err := client.CoreV1().Pods(gatekeeperNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to list Gatekeeper pods: %v", err), true
	}

	runningPods := 0
	totalPods := len(pods.Items)
	var podStatuses []string

	for _, pod := range pods.Items {
		status := string(pod.Status.Phase)
		ready := 0
		total := len(pod.Status.ContainerStatuses)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
		}
		if pod.Status.Phase == corev1.PodRunning && ready == total {
			runningPods++
		}
		podStatuses = append(podStatuses, fmt.Sprintf("- %s: %s (%d/%d ready)", pod.Name, status, ready, total))
	}

	if runningPods == totalPods && totalPods > 0 {
		sb.WriteString("**Status:** Installed and Healthy ✓\n\n")
	} else if totalPods > 0 {
		sb.WriteString("**Status:** Installed but Degraded ⚠\n\n")
	} else {
		sb.WriteString("**Status:** Namespace exists but no pods found\n\n")
	}

	_, _ = fmt.Fprintf(&sb, "**Pods:** %d/%d running\n\n", runningPods, totalPods)
	for _, status := range podStatuses {
		sb.WriteString(status + "\n")
	}

	// Check for ConstraintTemplates
	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		sb.WriteString("\nFailed to check ConstraintTemplates\n")
		return sb.String(), false
	}

	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	templates, err := dynClient.Resource(ctGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		sb.WriteString("\n**ConstraintTemplates:** Unable to list (may need permissions)\n")
	} else {
		_, _ = fmt.Fprintf(&sb, "\n**ConstraintTemplates:** %d installed\n", len(templates.Items))
		if len(templates.Items) > 0 {
			for _, t := range templates.Items {
				_, _ = fmt.Fprintf(&sb, "- %s\n", t.GetName())
			}
		}
	}

	// Check if ownership policy is installed
	_, err = dynClient.Resource(ctGVR).Get(ctx, ownershipTemplateName, metav1.GetOptions{})
	if err == nil {
		_, _ = fmt.Fprintf(&sb, "\n**Ownership Policy:** Installed (template: %s)\n", ownershipTemplateName)
	} else {
		sb.WriteString("\n**Ownership Policy:** Not installed\n")
		sb.WriteString("Use `install_ownership_policy` to set up ownership label enforcement.\n")
	}

	return sb.String(), false
}

func (s *Server) toolGetOwnershipPolicyStatus(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Ownership Policy Status\n\n")

	// Check ConstraintTemplate
	ctGVR := schema.GroupVersionResource{
		Group:    "templates.gatekeeper.sh",
		Version:  "v1",
		Resource: "constrainttemplates",
	}

	template, err := dynClient.Resource(ctGVR).Get(ctx, ownershipTemplateName, metav1.GetOptions{})
	if err != nil {
		sb.WriteString("**Template:** Not installed\n")
		sb.WriteString("\nThe ownership labels policy is not installed.\n")
		sb.WriteString("Use `install_ownership_policy` to set it up.\n")
		return sb.String(), false
	}

	// Get template status
	templateStatus, _, _ := unstructured.NestedMap(template.Object, "status")
	created, _, _ := unstructured.NestedBool(templateStatus, "created")
	_, _ = fmt.Fprintf(&sb, "**Template:** %s (created: %v)\n", ownershipTemplateName, created)

	// Check Constraint
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	constraint, err := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
	if err != nil {
		sb.WriteString("**Constraint:** Not created\n")
		sb.WriteString("\nTemplate exists but no constraint is active.\n")
		return sb.String(), false
	}

	// Get constraint spec
	spec, _, _ := unstructured.NestedMap(constraint.Object, "spec")
	enforcementAction, _, _ := unstructured.NestedString(spec, "enforcementAction")
	if enforcementAction == "" {
		enforcementAction = "deny"
	}

	_, _ = fmt.Fprintf(&sb, "**Constraint:** %s\n", ownershipConstraintName)
	_, _ = fmt.Fprintf(&sb, "**Mode:** %s\n", enforcementAction)

	// Get required labels
	params, _, _ := unstructured.NestedMap(spec, "parameters")
	labels, _, _ := unstructured.NestedStringSlice(params, "labels")
	if len(labels) > 0 {
		_, _ = fmt.Fprintf(&sb, "**Required Labels:** %s\n", strings.Join(labels, ", "))
	}

	// Get match configuration
	match, _, _ := unstructured.NestedMap(spec, "match")
	excludedNS, _, _ := unstructured.NestedStringSlice(match, "excludedNamespaces")
	if len(excludedNS) > 0 {
		_, _ = fmt.Fprintf(&sb, "**Excluded Namespaces:** %s\n", strings.Join(excludedNS, ", "))
	}

	// Get violation count from status
	status, _, _ := unstructured.NestedMap(constraint.Object, "status")
	totalViolations, found, _ := unstructured.NestedInt64(status, "totalViolations")
	if found {
		_, _ = fmt.Fprintf(&sb, "\n**Total Violations:** %d\n", totalViolations)
	}

	return sb.String(), false
}

func (s *Server) toolListOwnershipViolations(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespaceFilter, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	limit := int64(50)
	if v, ok := args["limit"].(float64); ok {
		limit = int64(v)
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	// Get Constraint
	constraintGVR := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	constraint, err := dynClient.Resource(constraintGVR).Get(ctx, ownershipConstraintName, metav1.GetOptions{})
	if err != nil {
		return "Ownership policy not installed. Use `install_ownership_policy` to set it up.", false
	}

	var sb strings.Builder
	sb.WriteString("# Ownership Label Violations\n\n")

	// Get enforcement mode
	spec, _, _ := unstructured.NestedMap(constraint.Object, "spec")
	enforcementAction, _, _ := unstructured.NestedString(spec, "enforcementAction")
	if enforcementAction == "" {
		enforcementAction = "deny"
	}
	_, _ = fmt.Fprintf(&sb, "**Mode:** %s\n", enforcementAction)

	// Get violations from status
	status, _, _ := unstructured.NestedMap(constraint.Object, "status")
	violations, _, _ := unstructured.NestedSlice(status, "violations")

	if len(violations) == 0 {
		sb.WriteString("\n**No violations found!** All resources have required ownership labels.\n")
		return sb.String(), false
	}

	totalViolations, _, _ := unstructured.NestedInt64(status, "totalViolations")
	_, _ = fmt.Fprintf(&sb, "**Total Violations:** %d\n\n", totalViolations)

	// Group by namespace
	type violation struct {
		Kind      string
		Name      string
		Namespace string
		Message   string
	}
	var violationList []violation
	namespaceCount := make(map[string]int)

	for _, v := range violations {
		vMap, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		ns, _, _ := unstructured.NestedString(vMap, "namespace")
		if namespaceFilter != "" && ns != namespaceFilter {
			continue
		}

		kind, _, _ := unstructured.NestedString(vMap, "kind")
		name, _, _ := unstructured.NestedString(vMap, "name")
		message, _, _ := unstructured.NestedString(vMap, "message")

		violationList = append(violationList, violation{
			Kind:      kind,
			Name:      name,
			Namespace: ns,
			Message:   message,
		})
		namespaceCount[ns]++
	}

	if len(violationList) == 0 {
		_, _ = fmt.Fprintf(&sb, "\nNo violations in namespace `%s`.\n", namespaceFilter)
		return sb.String(), false
	}

	// Show summary by namespace
	sb.WriteString("## By Namespace\n\n")
	for ns, count := range namespaceCount {
		_, _ = fmt.Fprintf(&sb, "- **%s**: %d violations\n", ns, count)
	}

	// Show details
	sb.WriteString("\n## Violations\n\n")
	sb.WriteString("| Namespace | Kind | Name | Issue |\n")
	sb.WriteString("|-----------|------|------|-------|\n")

	shown := int64(0)
	for _, v := range violationList {
		if shown >= limit {
			break
		}
		// Truncate message for table
		msg := v.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		_, _ = fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", v.Namespace, v.Kind, v.Name, msg)
		shown++
	}

	if int64(len(violationList)) > limit {
		_, _ = fmt.Fprintf(&sb, "\n*Showing %d of %d violations. Use `limit` parameter to see more.*\n", limit, len(violationList))
	}

	return sb.String(), false
}

func (s *Server) toolInstallOwnershipPolicy(ctx context.Context, args map[string]interface{}) (string, bool) {
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
	client, err := s.getClientForCluster(cluster)
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

	dynClient, err := s.getDynamicClientForCluster(cluster)
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

func (s *Server) toolSetOwnershipPolicyMode(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	mode, _ := args["mode"].(string)

	if mode == "" {
		return "mode is required (dryrun, warn, or enforce)", true
	}

	if mode != "dryrun" && mode != "warn" && mode != "enforce" {
		return "mode must be one of: dryrun, warn, enforce", true
	}

	dynClient, err := s.getDynamicClientForCluster(cluster)
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

func (s *Server) toolUninstallOwnershipPolicy(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)

	dynClient, err := s.getDynamicClientForCluster(cluster)
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

// GitOps Tools

