package server

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

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
