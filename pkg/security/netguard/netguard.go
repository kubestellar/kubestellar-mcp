// Package netguard owns the SSRF IP-blocklist predicate shared by every
// outbound-URL validator in this repo (Helm repo/OCI refs, GitOps repo
// URLs, and any future MCP tool that resolves a user-supplied hostname
// before contacting it). Extracted from pkg/deploy/mcp/helm and
// pkg/gitops, which had each grown their own copy of this check; the two
// copies had drifted apart (the gitops copy additionally blocked
// 0.0.0.0/"::" via net.IP.IsUnspecified, the helm copy did not), so a
// hostname resolving to the unspecified address could bypass the Helm-side
// SSRF guard while being correctly blocked on the GitOps side. See
// kubestellar/kubestellar-mcp#964 for the precedent of extracting a
// duplicated security predicate (pkg/security/namespace) for the same
// reason.
package netguard

import (
	"errors"
	"net"
)

// ErrBlockedIP is the sentinel every outbound-URL validator should wrap
// (via fmt.Errorf("...: %w", ErrBlockedIP)) when it rejects a hostname
// because IsBlockedIP returned true. Wrapping this sentinel - instead of
// only formatting the blocked IP into the error message - lets
// pkg/metrics.ClassifyError recognize an SSRF-guard rejection via
// errors.Is and record it under a dedicated, bounded error_kind label
// (metrics.ErrorKindBlockedIP), so operators can see the rate of blocked
// outbound-URL attempts in mcpserver_tool_errors_total without that signal
// being folded into the generic "unknown" bucket.
var ErrBlockedIP = errors.New("netguard: blocked IP address")

// cgnatNet is RFC 6598 Carrier-Grade NAT space (100.64.0.0/10). Not covered
// by net.IP.IsPrivate() but often routes to internal services.
var _, cgnatNet, _ = net.ParseCIDR("100.64.0.0/10")

// cloudMetaNet is the cloud instance metadata service (169.254.169.254/32).
// This is the primary SSRF target for credential theft in AWS, GCP, and Azure.
var _, cloudMetaNet, _ = net.ParseCIDR("169.254.169.254/32")

// ietfNet is RFC 6890 IETF Protocol Assignments (192.0.0.0/24).
var _, ietfNet, _ = net.ParseCIDR("192.0.0.0/24")

// IsBlockedIP returns true if ip must not be contacted by an outbound MCP
// tool (Helm repo/OCI pulls, GitOps git clone, or any similar
// resolve-then-connect path). Blocks loopback, private, link-local,
// unspecified (0.0.0.0 / ::), CGNAT, and cloud-metadata ranges.
func IsBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() ||
		cgnatNet.Contains(ip) || cloudMetaNet.Contains(ip) || ietfNet.Contains(ip)
}
