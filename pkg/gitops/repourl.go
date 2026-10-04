package gitops

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// allowedRepoSchemes restricts git clone to safe URL schemes.
// http://, file://, and ssh:// are blocked to prevent MITM, SSRF, and local file reads.
var allowedRepoSchemes = map[string]bool{
	"https": true,
}

var validGitBranchPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

var (
	// gitopsCGNATNet is RFC 6598 Carrier-Grade NAT space (100.64.0.0/10).
	_, gitopsCGNATNet, _ = net.ParseCIDR("100.64.0.0/10")
	// gitopsCloudMetaNet is the cloud instance metadata service (169.254.169.254/32).
	_, gitopsCloudMetaNet, _ = net.ParseCIDR("169.254.169.254/32")
	// gitopsIETFNet is RFC 6890 IETF Protocol Assignments (192.0.0.0/24).
	_, gitopsIETFNet, _ = net.ParseCIDR("192.0.0.0/24")
)

// gitopsDNSTimeout bounds hostname resolution for repo URL validation.
const gitopsDNSTimeout = 3 * time.Second

// isGitopsBlockedIP returns true if ip falls into a range that must not be
// contacted by git clone (loopback, private, link-local, CGNAT, cloud metadata).
func isGitopsBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() ||
		gitopsCGNATNet.Contains(ip) || gitopsCloudMetaNet.Contains(ip) || gitopsIETFNet.Contains(ip)
}

// validateRepoURLWithSchemes validates a repo URL against a custom scheme
// allowlist to prevent SSRF, local file reads, and arbitrary SSH
// connections. Production callers pass allowedRepoSchemes (https-only);
// tests may pass a wider or narrower set.
func validateRepoURLWithSchemes(repo string, schemes map[string]bool) error {
	if repo == "" {
		return fmt.Errorf("repo URL is required")
	}
	u, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("invalid repo URL: %w", err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("repo URL must include a scheme (e.g., https://); got %q", repo)
	}
	if !schemes[u.Scheme] {
		return fmt.Errorf("repo URL scheme %q is not allowed; only https:// is permitted", u.Scheme)
	}
	// file:// URLs don't have a host — skip host check for file scheme
	if u.Scheme != "file" && u.Host == "" {
		return fmt.Errorf("repo URL must include a host; got %q", repo)
	}

	// SSRF protection: resolve the hostname and reject private/internal IPs.
	// Prevents git clone from reaching cloud metadata, RFC 1918, CGNAT, and
	// other internal services via DNS rebinding (#276).
	host := u.Hostname()
	if host != "" {
		if ip := net.ParseIP(host); ip != nil {
			if isGitopsBlockedIP(ip) {
				return fmt.Errorf("repo URL %q resolves to blocked IP %s (private/internal address)", repo, ip)
			}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), gitopsDNSTimeout)
			defer cancel()
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return fmt.Errorf("repo URL %q: DNS lookup failed — cannot verify safety: %w", repo, err)
			}
			for _, ipStr := range ips {
				if ip := net.ParseIP(ipStr); ip != nil && isGitopsBlockedIP(ip) {
					return fmt.Errorf("repo URL %q resolves to blocked IP %s (private/internal address)", repo, ip)
				}
			}
		}
	}
	return nil
}

func validateBranchName(branch string) error {
	if branch == "" {
		return nil
	}
	if strings.HasPrefix(branch, "-") || !validGitBranchPattern.MatchString(branch) {
		return fmt.Errorf("invalid git branch name %q: only letters, numbers, dots, underscores, slashes, and hyphens are allowed", branch)
	}
	return nil
}

// revalidateRepoHost re-resolves the repo URL's hostname and re-applies the
// isGitopsBlockedIP check immediately before git clone is exec'd, narrowing
// the TOCTOU window against DNS rebinding attacks that flip a benign A record
// to a blocked address (cloud metadata, RFC 1918, CGNAT, loopback) between
// validateRepoURLWithSchemes and git's own network resolution. Mirrors the
// helm-side mitigation in pkg/deploy/mcp/tools_helm.go: revalidateHelmHosts.
// See issue #884.
func revalidateRepoHost(repo string) error {
	if repo == "" {
		return nil
	}
	u, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("unparseable repo URL")
	}
	// file:// URLs have no host and are only used by tests via
	// NewManifestReaderWithSchemes; they cannot rebind DNS, so skip.
	if u.Scheme == "file" || u.Hostname() == "" {
		return nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isGitopsBlockedIP(ip) {
			return fmt.Errorf("resolves to blocked IP %s (private/internal address)", ip)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitopsDNSTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %q: %w", host, err)
	}
	for _, ipStr := range ips {
		if ip := net.ParseIP(ipStr); ip != nil && isGitopsBlockedIP(ip) {
			return fmt.Errorf("resolves to blocked IP %s (private/internal address)", ip)
		}
	}
	return nil
}
