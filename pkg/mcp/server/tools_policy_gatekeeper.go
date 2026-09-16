package server

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
