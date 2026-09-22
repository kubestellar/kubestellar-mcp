package mcp

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	nsval "github.com/kubestellar/kubestellar-mcp/pkg/security/namespace"
)

var (
	// helmBlockedCGNATNet is RFC 6598 Carrier-Grade NAT space (100.64.0.0/10).
	// Not covered by net.IP.IsPrivate() but often routes to internal services.
	_, helmBlockedCGNATNet, _ = net.ParseCIDR("100.64.0.0/10")

	// helmBlockedCloudMetaNet is the cloud instance metadata service (169.254.169.254/32).
	// This is the primary SSRF target for credential theft in AWS, GCP, and Azure.
	_, helmBlockedCloudMetaNet, _ = net.ParseCIDR("169.254.169.254/32")

	// helmBlockedIETFNet is RFC 6890 IETF Protocol Assignments (192.0.0.0/24).
	_, helmBlockedIETFNet, _ = net.ParseCIDR("192.0.0.0/24")

	// helmHostResolver can be replaced in tests to avoid real DNS lookups.
	helmHostResolver = net.LookupHost
)

// isHelmBlockedIP returns true if the resolved IP must not be contacted by the
// Helm proxy. Blocks loopback, private, link-local, CGNAT, and cloud-metadata ranges.
func isHelmBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		helmBlockedCGNATNet.Contains(ip) ||
		helmBlockedCloudMetaNet.Contains(ip) ||
		helmBlockedIETFNet.Contains(ip)
}

// validateHelmChartRef ensures the chart positional argument is safe.
// It blocks local filesystem paths (including nested path traversal), flag
// injection via leading "-", and for oci:// references applies the same
// IP-blocking rules as validateHelmRepoURL to prevent SSRF via OCI
// registries that resolve to private/internal addresses (see #246).
func validateHelmChartRef(chart string) error {
	// Block flag injection: chart is passed as a helm argv element and a
	// leading "-" would be parsed as a CLI flag (same class as #269).
	if strings.HasPrefix(chart, "-") {
		return fmt.Errorf("helm chart ref %q must not begin with '-' (possible flag injection)", chart)
	}

	// Block local filesystem paths and any ".." segment (not only a "../" prefix),
	// so values like foo/../../../etc/passwd cannot bypass the check.
	if strings.HasPrefix(chart, "/") || strings.HasPrefix(chart, "./") || strings.Contains(chart, "..") {
		return fmt.Errorf("helm chart ref %q is a local path \u2014 only remote chart names or oci:// references are allowed", chart)
	}

	// For OCI references, validate the registry URL the same way as --repo.
	if strings.HasPrefix(chart, "oci://") {
		u, err := url.Parse(chart)
		if err != nil {
			return fmt.Errorf("invalid oci chart ref: %w", err)
		}
		if u.Host == "" {
			return fmt.Errorf("oci chart ref must include a registry host; got %q", chart)
		}
		hostname := u.Hostname()
		if ip := net.ParseIP(hostname); ip != nil {
			if isHelmBlockedIP(ip) {
				return fmt.Errorf("oci chart ref %q uses a blocked IP address", chart)
			}
			return nil
		}
		addrs, err := helmHostResolver(hostname)
		if err != nil {
			return fmt.Errorf("oci chart ref host %q could not be resolved: %w", hostname, err)
		}
		for _, addr := range addrs {
			ip := net.ParseIP(addr)
			if ip == nil {
				continue
			}
			if isHelmBlockedIP(ip) {
				return fmt.Errorf("oci chart ref %q resolves to blocked IP %s (private/internal address)", chart, ip)
			}
		}
	}

	return nil
}

// validateHelmRepoURL ensures the Helm --repo URL is safe to contact.
// Only https:// is allowed. The hostname is resolved (via helmHostResolver) and
// any private, loopback, link-local, CGNAT, or cloud-metadata IP is rejected to
// prevent SSRF / cloud IMDS credential theft (see #216).
func validateHelmRepoURL(repo string) error {
	u, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("invalid repo URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("helm repo URL scheme %q is not allowed; use https://", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("helm repo URL must include a host; got %q", repo)
	}

	hostname := u.Hostname()

	// If the host is already an IP literal, check it directly (no DNS lookup needed).
	if ip := net.ParseIP(hostname); ip != nil {
		if isHelmBlockedIP(ip) {
			return fmt.Errorf("helm repo URL %q uses a blocked IP address", repo)
		}
		return nil
	}

	// Resolve the hostname and block any private/internal IPs (SSRF protection).
	addrs, err := helmHostResolver(hostname)
	if err != nil {
		return fmt.Errorf("helm repo URL host %q could not be resolved: %w", hostname, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if isHelmBlockedIP(ip) {
			return fmt.Errorf("helm repo URL %q resolves to blocked IP %s (private/internal address)", repo, ip)
		}
	}
	return nil
}

// revalidateHelmHosts performs a second DNS resolution check on the chart and
// repo hostnames immediately before exec. This narrows the TOCTOU window
// between the initial validation (validateHelmChartRef/validateHelmRepoURL)
// and helm's own DNS resolution. If a DNS rebinding attack switched the
// record to a blocked IP after validation, this check catches it (#275).
func revalidateHelmHosts(chart, repo string) error {
	// Re-check OCI chart hostname if applicable.
	if strings.HasPrefix(chart, "oci://") {
		ref := strings.TrimPrefix(chart, "oci://")
		host := strings.SplitN(ref, "/", 2)[0]
		// Strip port if present.
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "" {
			if err := resolveAndBlock(host); err != nil {
				return fmt.Errorf("chart host %q: %w", host, err)
			}
		}
	}
	// Re-check --repo hostname if specified.
	if repo != "" {
		u, err := url.Parse(repo)
		if err == nil && u.Hostname() != "" {
			if err := resolveAndBlock(u.Hostname()); err != nil {
				return fmt.Errorf("repo host %q: %w", u.Hostname(), err)
			}
		}
	}
	return nil
}

// resolveAndBlock resolves a hostname and returns an error if any resulting IP
// is in a blocked range.
func resolveAndBlock(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if isHelmBlockedIP(ip) {
			return fmt.Errorf("resolves to blocked IP %s", ip)
		}
		return nil
	}
	addrs, err := helmHostResolver(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil && isHelmBlockedIP(ip) {
			return fmt.Errorf("resolves to blocked IP %s", ip)
		}
	}
	return nil
}

// validHelmIdentifierPattern enforces Kubernetes DNS label format for Helm
// release names, namespaces, and kube-context names. This prevents flag-injection
// attacks where a leading "--" value would be parsed as a CLI flag by helm.
// See: https://github.com/kubestellar/kubestellar-mcp/issues/269
var validHelmIdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-\.]*[a-z0-9]$|^[a-z0-9]$`)

// helmSetSpecialChars are characters that Helm's --set parser treats as
// structural delimiters: comma (list separator), braces (set syntax),
// brackets (index syntax), and backtick (raw string quoting). Any of these
// appearing in a user-supplied key or value would let an attacker inject extra
// key=value pairs or override values beyond what was intended.
// See: https://github.com/kubestellar/kubestellar-mcp/issues/288
const helmSetSpecialChars = ",{}[]`"

// validateHelmIdentifier rejects values that would be misinterpreted as CLI flags
// (leading "-") or that do not conform to Kubernetes DNS label / subdomain format.
func validateHelmIdentifier(kind, value string) error {
	if value == "" {
		return nil // empty values are handled by existing required-field checks
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s %q must not begin with '-' (possible flag injection)", kind, value)
	}
	if !validHelmIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s %q is not a valid Kubernetes identifier (must be lowercase alphanumeric, hyphens, or dots, and start/end with alphanumeric)", kind, value)
	}
	return nil
}

// validateHelmSetKey rejects --set keys that contain Helm structural characters.
// Keys may contain dots (path separators) and brackets (array indices), both of
// which are normal Helm key syntax, but commas, braces, and backticks are not
// valid in keys and indicate injection attempts.
func validateHelmSetKey(key string) error {
	if strings.HasPrefix(key, "-") {
		return fmt.Errorf("--set key %q must not begin with '-' (possible flag injection)", key)
	}
	for _, ch := range ",{}` " {
		if strings.ContainsRune(key, ch) {
			return fmt.Errorf("--set key %q contains forbidden character %q (possible Helm value injection)", key, string(ch))
		}
	}
	return nil
}

// validateHelmSetValue rejects --set values that contain Helm structural
// characters. A comma in a value causes Helm to split it into multiple
// key=value pairs, allowing injection of extra chart values (#288).
func validateHelmSetValue(value string) error {
	for _, ch := range helmSetSpecialChars {
		if strings.ContainsRune(value, ch) {
			return fmt.Errorf("--set value %q contains forbidden character %q (possible Helm value injection; use values_yaml for complex values)", value, string(ch))
		}
	}
	return nil
}

// validateHelmClusters validates user-supplied cluster names (#289).
// Cluster names are passed as --kube-context values; they must meet the same
// Kubernetes identifier rules as release names and namespaces.
func validateHelmClusters(clusters []string) error {
	for _, c := range clusters {
		if err := validateHelmIdentifier("cluster", c); err != nil {
			return err
		}
	}
	return nil
}

// validateHelmInstallParams runs all of the SSRF and flag-injection guards
// against a helm_install request before any cluster is touched.
func validateHelmInstallParams(params helmInstallParams) error {
	// Validate namespace to prevent access to system namespaces (#377).
	if err := nsval.ValidateNamespace(params.Namespace); err != nil {
		return fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate chart ref to prevent local filesystem access and OCI SSRF (see #246).
	if err := validateHelmChartRef(params.Chart); err != nil {
		return fmt.Errorf("invalid chart ref: %w", err)
	}

	// Validate repo URL to prevent SSRF and local file reads via file:// or ssh://
	if params.Repo != "" {
		if err := validateHelmRepoURL(params.Repo); err != nil {
			return fmt.Errorf("invalid repo URL: %w", err)
		}
	}

	// Validate identifiers against Kubernetes naming rules to prevent flag injection (#269).
	if err := validateHelmIdentifier("release_name", params.ReleaseName); err != nil {
		return err
	}
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return err
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return err
	}

	// Validate --set keys and values to prevent Helm value injection (#288).
	for k, v := range params.Values {
		if err := validateHelmSetKey(k); err != nil {
			return err
		}
		if err := validateHelmSetValue(v); err != nil {
			return err
		}
	}

	return nil
}
