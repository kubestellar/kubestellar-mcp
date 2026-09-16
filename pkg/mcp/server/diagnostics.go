package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) toolGetWarningEvents(ctx context.Context, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, err := extractAndValidateNamespace(args)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	involvedObject, _ := args["involved_object"].(string)
	limit := int64(50)
	if v, ok := args["limit"].(float64); ok {
		limit = int64(v)
	}

	client, err := s.getClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	listOpts := metav1.ListOptions{
		FieldSelector: "type=Warning",
		Limit:         limit,
	}

	var events *corev1.EventList
	if namespace == "" {
		events, err = client.CoreV1().Events("").List(ctx, listOpts)
	} else {
		events, err = client.CoreV1().Events(namespace).List(ctx, listOpts)
	}

	if err != nil {
		return fmt.Sprintf("Failed to list events: %v", err), true
	}

	var sb strings.Builder
	count := 0

	for _, event := range events.Items {
		// Filter by involved object if specified
		if involvedObject != "" && event.InvolvedObject.Name != involvedObject {
			continue
		}

		count++
		age := ""
		if event.LastTimestamp.Time.IsZero() {
			age = "unknown"
		} else {
			age = formatAge(event.LastTimestamp.Time)
		}

		_, _ = fmt.Fprintf(&sb, "⚠️  [%s] %s/%s\n", age, event.InvolvedObject.Kind, event.InvolvedObject.Name)
		_, _ = fmt.Fprintf(&sb, "   %s: %s\n", event.Reason, event.Message)
		if event.Count > 1 {
			_, _ = fmt.Fprintf(&sb, "   (occurred %d times)\n", event.Count)
		}
		sb.WriteString("\n")
	}

	if count == 0 {
		return "✅ No warning events found", false
	}

	header := fmt.Sprintf("Found %d warning events:\n\n", count)
	return header + sb.String(), false
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
