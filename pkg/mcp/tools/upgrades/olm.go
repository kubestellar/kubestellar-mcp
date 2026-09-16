package upgrades

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CheckOLMOperatorUpgrades checks OLM operators for available upgrades.
func CheckOLMOperatorUpgrades(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	dynClient, err := ca.GetDynamicClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# OLM Operator Upgrades\n\n")

	var subscriptions *unstructured.UnstructuredList
	if namespace == "" {
		subscriptions, err = dynClient.Resource(subscriptionGVR).Namespace("").List(ctx, metav1.ListOptions{})
	} else {
		subscriptions, err = dynClient.Resource(subscriptionGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}

	if err != nil {
		if strings.Contains(err.Error(), "could not find the requested resource") ||
			strings.Contains(err.Error(), "no matches for kind") {
			sb.WriteString("**OLM Status:** Not installed\n\n")
			sb.WriteString("Operator Lifecycle Manager (OLM) is not installed on this cluster.\n")
			sb.WriteString("OLM is required for managing operators through subscriptions.\n\n")
			sb.WriteString("To install OLM, visit: https://olm.operatorframework.io/docs/getting-started/\n")
			return sb.String(), false
		}
		return fmt.Sprintf("Failed to list subscriptions: %v", err), true
	}

	if len(subscriptions.Items) == 0 {
		sb.WriteString("**OLM Status:** Installed\n")
		sb.WriteString("**Subscriptions Found:** 0\n\n")
		sb.WriteString("No operator subscriptions found.\n")
		return sb.String(), false
	}

	sb.WriteString("**OLM Status:** Installed\n")
	_, _ = fmt.Fprintf(&sb, "**Subscriptions Found:** %d\n\n", len(subscriptions.Items))

	sb.WriteString("| Operator | Namespace | Current CSV | Channel | Auto-Update | Status |\n")
	sb.WriteString("|----------|-----------|-------------|---------|-------------|--------|\n")

	upgradesPending := 0
	for _, sub := range subscriptions.Items {
		name := sub.GetName()
		ns := sub.GetNamespace()

		spec, _, _ := unstructured.NestedMap(sub.Object, "spec")
		channel, _, _ := unstructured.NestedString(spec, "channel")
		installPlanApproval, _, _ := unstructured.NestedString(spec, "installPlanApproval")
		autoUpdate := installPlanApproval == "Automatic"

		status, _, _ := unstructured.NestedMap(sub.Object, "status")
		currentCSV, _, _ := unstructured.NestedString(status, "currentCSV")
		state, _, _ := unstructured.NestedString(status, "state")

		statusEmoji := ""
		switch state {
		case "AtLatestKnown":
			statusEmoji = "Up to date"
		case "UpgradePending":
			statusEmoji = "Upgrade pending"
			upgradesPending++
		case "UpgradeAvailable":
			statusEmoji = "Upgrade available"
			upgradesPending++
		default:
			statusEmoji = state
		}

		autoUpdateStr := "Manual"
		if autoUpdate {
			autoUpdateStr = "Auto"
		}

		_, _ = fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s |\n",
			name, ns, currentCSV, channel, autoUpdateStr, statusEmoji)
	}

	sb.WriteString("\n")
	if upgradesPending > 0 {
		_, _ = fmt.Fprintf(&sb, "**Upgrades Available:** %d operator(s) have pending upgrades\n", upgradesPending)
	} else {
		sb.WriteString("**Upgrades Available:** All operators are at their latest known version\n")
	}

	return sb.String(), false
}
