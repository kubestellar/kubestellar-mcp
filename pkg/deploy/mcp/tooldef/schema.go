package tooldef

// DryRunProperty returns the standard "dry_run" boolean property shared by
// every tool that can preview changes before applying them. Before this
// helper existed, each domain's register.go re-declared this exact literal
// (same type, same description) independently, letting the wording drift
// across packages without any single place to fix it.
func DryRunProperty() map[string]interface{} {
	return map[string]interface{}{
		"type":        "boolean",
		"description": "Preview changes without applying",
	}
}

// ClustersProperty returns the standard "clusters" array-of-strings property
// shared by every multi-cluster tool, parameterized by description since
// different tools scope "not specified" differently (e.g. "all clusters"
// vs. "clusters where release exists").
func ClustersProperty(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"items":       map[string]interface{}{"type": "string"},
		"description": description,
	}
}
