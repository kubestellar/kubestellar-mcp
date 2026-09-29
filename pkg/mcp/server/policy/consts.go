package policy

// OPA Gatekeeper constants shared across the ownership-policy handlers
// (gatekeeper.go, lifecycle.go, status.go). Extracted from
// pkg/mcp/server/tools_rbac_audit.go (which only declared them; every use
// site was already in the policy files) as part of the pkg/mcp/server
// decomposition — kubestellar-mcp#1027.
const (
	gatekeeperNamespace          = "gatekeeper-system"
	ownershipTemplateName        = "k8srequiredlabels"
	ownershipConstraintName      = "require-ownership-labels"
	constraintTemplateAPIVersion = "templates.gatekeeper.sh/v1"
	constraintAPIVersion         = "constraints.gatekeeper.sh/v1beta1"
)
