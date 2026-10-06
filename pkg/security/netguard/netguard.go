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
	"context"
	"errors"
	"fmt"
	"net"
	"time"
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

// Resolver matches net.DefaultResolver.LookupHost's signature, letting
// callers inject a deterministic stand-in in tests instead of depending on
// real DNS.
type Resolver func(ctx context.Context, host string) (addrs []string, err error)

// DefaultResolver resolves hostnames with the standard library's
// context-aware DNS resolver.
var DefaultResolver Resolver = net.DefaultResolver.LookupHost

// DefaultDNSTimeout bounds hostname resolution in ResolveAndBlock when the
// caller's context carries no deadline of its own, so a slow or
// blackholed DNS server cannot stall an outbound tool call indefinitely.
const DefaultDNSTimeout = 3 * time.Second

// ResolveAndBlock resolves host - or parses it directly if it is already a
// literal IP - and returns an error wrapping ErrBlockedIP if any resulting
// address falls into a blocked range (see IsBlockedIP).
//
// This is the single re-check every outbound-URL validator in this repo
// (Helm repo/OCI refs, GitOps repo URLs) runs immediately before the
// subprocess that actually connects, to narrow the TOCTOU window between
// earlier input validation and that subprocess's own DNS resolution: if DNS
// has rebound to a blocked address (cloud metadata, RFC 1918, CGNAT,
// loopback) in between, this call catches it. It was extracted from two
// near-identical copies (pkg/deploy/mcp/helm and pkg/gitops) that had
// drifted: the Helm copy resolved via the un-timed net.LookupHost with no
// bound on a hung DNS server, while the GitOps copy applied a 3s context
// timeout. Both now share this one bounded implementation.
//
// If resolve is nil, DefaultResolver is used. If ctx carries no deadline,
// DefaultDNSTimeout is applied.
func ResolveAndBlock(ctx context.Context, host string, resolve Resolver) error {
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return fmt.Errorf("resolves to blocked IP %s: %w", ip, ErrBlockedIP)
		}
		return nil
	}
	if resolve == nil {
		resolve = DefaultResolver
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultDNSTimeout)
		defer cancel()
	}
	addrs, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %q: %w", host, err)
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil && IsBlockedIP(ip) {
			return fmt.Errorf("resolves to blocked IP %s: %w", ip, ErrBlockedIP)
		}
	}
	return nil
}
