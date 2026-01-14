package claude

import (
	"fmt"
	"strings"
)

// ClusterContext contains information about the Kubernetes cluster context
type ClusterContext struct {
	Clusters         []string
	CurrentCluster   string
	CurrentNamespace string
	Resources        map[string]interface{} // Optional: pre-fetched resources
}

// BuildSystemPrompt creates the system prompt for Kubernetes queries
func BuildSystemPrompt(ctx ClusterContext) string {
	var sb strings.Builder

	sb.WriteString(`You are kubectl-claude, an AI assistant for multi-cluster Kubernetes administration.

## Your Capabilities
You help users with:
1. Understanding Kubernetes resources and their status
2. Troubleshooting issues across clusters
3. Suggesting kubectl commands to achieve their goals
4. Explaining Kubernetes concepts and best practices
5. Analyzing cluster health and resource usage

## Response Guidelines
- Be concise and actionable
- When suggesting commands, format them in code blocks
- If multiple clusters are involved, clearly indicate which cluster each command targets
- Always consider security implications and warn about destructive operations
- If you need more information to help, ask clarifying questions

`)

	// Add cluster context
	sb.WriteString("## Current Context\n")

	if ctx.CurrentCluster != "" {
		sb.WriteString(fmt.Sprintf("- Current cluster: %s\n", ctx.CurrentCluster))
	}

	if ctx.CurrentNamespace != "" {
		sb.WriteString(fmt.Sprintf("- Current namespace: %s\n", ctx.CurrentNamespace))
	}

	if len(ctx.Clusters) > 0 {
		sb.WriteString(fmt.Sprintf("- Available clusters: %s\n", strings.Join(ctx.Clusters, ", ")))
	}

	sb.WriteString("\n")

	return sb.String()
}

// BuildQueryPrompt formats a user query with optional context
func BuildQueryPrompt(query string, additionalContext string) string {
	if additionalContext == "" {
		return query
	}

	return fmt.Sprintf(`%s

Additional context from the cluster:
%s`, query, additionalContext)
}

// FormatResourceContext formats Kubernetes resources for inclusion in prompts
func FormatResourceContext(resourceType string, resources interface{}) string {
	// This can be expanded to format different resource types appropriately
	return fmt.Sprintf("Current %s state:\n%v", resourceType, resources)
}

// CommonPrompts contains reusable prompt fragments
var CommonPrompts = struct {
	TroubleshootPrefix string
	ExplainPrefix      string
	CommandPrefix      string
	AnalyzePrefix      string
}{
	TroubleshootPrefix: "Help me troubleshoot: ",
	ExplainPrefix:      "Explain: ",
	CommandPrefix:      "What kubectl command should I use to: ",
	AnalyzePrefix:      "Analyze the following and provide insights: ",
}
