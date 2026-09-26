package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/server/handlers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toolFindResourceOwners(ctx context.Context, d *handlers.Deps, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	resourceType, _ := args["resource_type"].(string)

	if namespace == "" {
		return "namespace is required", true
	}

	if resourceType == "" {
		resourceType = "all"
	}

	client, err := d.GetClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	type resourceOwner struct {
		Kind       string
		Name       string
		Namespace  string
		Manager    string
		CreatedBy  string
		ManagedBy  string
		Owner      string
		Team       string
		LastUpdate string
	}

	owners := []resourceOwner{}

	// Common ownership labels/annotations
	ownershipLabels := []string{
		"app.kubernetes.io/managed-by",
		"owner",
		"team",
		"created-by",
		"managed-by",
	}
	ownershipAnnotations := []string{
		"kubectl.kubernetes.io/last-applied-configuration",
		"meta.helm.sh/release-name",
		"deployment.kubernetes.io/revision",
	}
	_ = ownershipAnnotations // used for context in output

	// Helper to extract owner info from resource metadata
	extractOwnerInfo := func(kind, name, ns string, labels, annotations map[string]string, managedFields []metav1.ManagedFieldsEntry) resourceOwner {
		ro := resourceOwner{
			Kind:      kind,
			Name:      name,
			Namespace: ns,
		}

		// Check managed fields for last manager
		if len(managedFields) > 0 {
			lastField := managedFields[len(managedFields)-1]
			ro.Manager = lastField.Manager
			if lastField.Time != nil {
				ro.LastUpdate = lastField.Time.Format("2006-01-02 15:04:05")
			}
		}

		// Check labels
		for _, label := range ownershipLabels {
			if val, ok := labels[label]; ok {
				switch label {
				case "app.kubernetes.io/managed-by", "managed-by":
					ro.ManagedBy = val
				case "owner", "created-by":
					ro.CreatedBy = val
				case "team":
					ro.Team = val
				}
			}
		}

		// Check annotations
		if val, ok := annotations["meta.helm.sh/release-name"]; ok {
			ro.ManagedBy = "helm:" + val
		}

		return ro
	}

	// Get Pods
	if resourceType == "all" || resourceType == "pods" {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to list pods: %v", err), true
		}
		for _, pod := range pods.Items {
			ro := extractOwnerInfo("Pod", pod.Name, pod.Namespace, pod.Labels, pod.Annotations, pod.ManagedFields)
			// Check owner references
			for _, ownerRef := range pod.OwnerReferences {
				ro.Owner = fmt.Sprintf("%s/%s", ownerRef.Kind, ownerRef.Name)
				break
			}
			owners = append(owners, ro)
		}
	}

	// Get Deployments
	if resourceType == "all" || resourceType == "deployments" {
		deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to list deployments: %v", err), true
		}
		for _, dep := range deployments.Items {
			ro := extractOwnerInfo("Deployment", dep.Name, dep.Namespace, dep.Labels, dep.Annotations, dep.ManagedFields)
			for _, ownerRef := range dep.OwnerReferences {
				ro.Owner = fmt.Sprintf("%s/%s", ownerRef.Kind, ownerRef.Name)
				break
			}
			owners = append(owners, ro)
		}
	}

	// Get Services
	if resourceType == "all" || resourceType == "services" {
		services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Sprintf("Failed to list services: %v", err), true
		}
		for _, svc := range services.Items {
			ro := extractOwnerInfo("Service", svc.Name, svc.Namespace, svc.Labels, svc.Annotations, svc.ManagedFields)
			for _, ownerRef := range svc.OwnerReferences {
				ro.Owner = fmt.Sprintf("%s/%s", ownerRef.Kind, ownerRef.Name)
				break
			}
			owners = append(owners, ro)
		}
	}

	if len(owners) == 0 {
		return "No resources found in namespace " + namespace, false
	}

	// Build output
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "# Resource Ownership in namespace: %s\n\n", namespace)
	_, _ = fmt.Fprintf(&sb, "Found %d resources\n\n", len(owners))

	// Group by manager
	managerGroups := make(map[string][]resourceOwner)
	for _, ro := range owners {
		manager := ro.Manager
		if manager == "" {
			manager = "(unknown)"
		}
		managerGroups[manager] = append(managerGroups[manager], ro)
	}

	sb.WriteString("## By Manager/Controller\n\n")
	for manager, resources := range managerGroups {
		_, _ = fmt.Fprintf(&sb, "### %s\n", manager)
		for _, ro := range resources {
			_, _ = fmt.Fprintf(&sb, "- **%s/%s**", ro.Kind, ro.Name)
			if ro.Owner != "" {
				_, _ = fmt.Fprintf(&sb, " (owner: %s)", ro.Owner)
			}
			if ro.ManagedBy != "" {
				_, _ = fmt.Fprintf(&sb, " [managed-by: %s]", ro.ManagedBy)
			}
			if ro.Team != "" {
				_, _ = fmt.Fprintf(&sb, " [team: %s]", ro.Team)
			}
			if ro.CreatedBy != "" {
				_, _ = fmt.Fprintf(&sb, " [created-by: %s]", ro.CreatedBy)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Summary table
	sb.WriteString("## Ownership Labels Summary\n\n")
	sb.WriteString("| Kind | Name | Manager | Owner | Managed-By | Team | Last Update |\n")
	sb.WriteString("|------|------|---------|-------|------------|------|-------------|\n")
	for _, ro := range owners {
		manager := ro.Manager
		if manager == "" {
			manager = "-"
		}
		owner := ro.Owner
		if owner == "" {
			owner = "-"
		}
		managedBy := ro.ManagedBy
		if managedBy == "" {
			managedBy = "-"
		}
		team := ro.Team
		if team == "" {
			team = "-"
		}
		lastUpdate := ro.LastUpdate
		if lastUpdate == "" {
			lastUpdate = "-"
		}
		_, _ = fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s | %s |\n",
			ro.Kind, ro.Name, manager, owner, managedBy, team, lastUpdate)
	}

	return sb.String(), false
}
