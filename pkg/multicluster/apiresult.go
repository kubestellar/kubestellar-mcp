package multicluster

import "strings"

// ClassifyMutationResult maps the error from a single-cluster write (patch,
// delete, etc.) to the repo's shared three-way per-resource status
// convention: nil err reports successStatus with no message; an error whose
// text contains "not found" reports "not-found" with notFoundMessage; any
// other error reports "failed" with err's message. It centralizes a mapping
// that was previously duplicated, with drifting literal status strings,
// across pkg/deploy/mcp/kubectl and pkg/deploy/mcp/labels.
func ClassifyMutationResult(err error, successStatus, notFoundMessage string) (status, message string) {
	if err == nil {
		return successStatus, ""
	}
	if strings.Contains(err.Error(), "not found") {
		return "not-found", notFoundMessage
	}
	return "failed", err.Error()
}
