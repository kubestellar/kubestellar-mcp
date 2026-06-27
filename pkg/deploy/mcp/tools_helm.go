package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
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
// It blocks local filesystem paths and, for oci:// references, applies the
// same IP-blocking rules as validateHelmRepoURL to prevent SSRF via OCI
// registries that resolve to private/internal addresses (see #246).
func validateHelmChartRef(chart string) error {
	// Block local filesystem paths.
	if strings.HasPrefix(chart, "/") || strings.HasPrefix(chart, "./") || strings.HasPrefix(chart, "../") {
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

// HelmReleaseInfo represents information about a Helm release
type HelmReleaseInfo struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   string `json:"revision"`
	Status     string `json:"status"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version"`
}

// HelmResult represents the result of a Helm operation
type HelmResult struct {
	Cluster     string `json:"cluster"`
	ReleaseName string `json:"release_name"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}

// handleHelmInstall installs a Helm chart to clusters
func (s *Server) handleHelmInstall(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		ReleaseName string            `json:"release_name"`
		Chart       string            `json:"chart"`
		Namespace   string            `json:"namespace"`
		Values      map[string]string `json:"values"`
		ValuesYAML  string            `json:"values_yaml"`
		Version     string            `json:"version"`
		Repo        string            `json:"repo"`
		Wait        bool              `json:"wait"`
		Timeout     string            `json:"timeout"`
		DryRun      bool              `json:"dry_run"`
		Clusters    []string          `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ReleaseName == "" || params.Chart == "" {
		return nil, fmt.Errorf("release_name and chart are required")
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if err := server.ValidateNamespace(params.Namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate chart ref to prevent local filesystem access and OCI SSRF (see #246).
	if err := validateHelmChartRef(params.Chart); err != nil {
		return nil, fmt.Errorf("invalid chart ref: %w", err)
	}

	// Validate repo URL to prevent SSRF and local file reads via file:// or ssh://
	if params.Repo != "" {
		if err := validateHelmRepoURL(params.Repo); err != nil {
			return nil, fmt.Errorf("invalid repo URL: %w", err)
		}
	}

	// Validate identifiers against Kubernetes naming rules to prevent flag injection (#269).
	if err := validateHelmIdentifier("release_name", params.ReleaseName); err != nil {
		return nil, err
	}
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return nil, err
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return nil, err
	}

	// Validate --set keys and values to prevent Helm value injection (#288).
	for k, v := range params.Values {
		if err := validateHelmSetKey(k); err != nil {
			return nil, err
		}
		if err := validateHelmSetValue(v); err != nil {
			return nil, err
		}
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("no clusters available")
	}

	var results []HelmResult
	for _, cluster := range targetClusters {
		result := s.helmInstall(ctx, cluster, params.ReleaseName, params.Chart, params.Namespace,
			params.Values, params.ValuesYAML, params.Version, params.Repo, params.Wait, params.Timeout, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "installed" || r.Status == "upgraded" || r.Status == "would-install" {
			successCount++
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
}

// helmInstall runs helm install/upgrade for a single cluster
func (s *Server) helmInstall(ctx context.Context, cluster, releaseName, chart, namespace string,
	values map[string]string, valuesYAML, version, repo string, wait bool, timeout string, dryRun bool) HelmResult {

	// Pre-exec DNS re-validation: re-resolve hostnames immediately before
	// exec to close the TOCTOU gap between validateHelmRepoURL/validateHelmChartRef
	// (which resolve during input validation) and the helm subprocess (which
	// resolves independently). If DNS has rebind to a blocked IP between
	// validation and now, abort. See #275.
	if err := revalidateHelmHosts(chart, repo); err != nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "failed",
			Message:     fmt.Sprintf("pre-exec SSRF re-check failed (possible DNS rebinding): %v", err),
		}
	}

	cmdArgs := []string{"upgrade", "--install", releaseName, chart,
		"--namespace", namespace,
		"--create-namespace",
		"--kube-context", cluster,
	}

	// Add repo if specified (already validated by handleHelmInstall)
	if repo != "" {
		cmdArgs = append(cmdArgs, "--repo", repo)
	}

	// Add version if specified
	if version != "" {
		cmdArgs = append(cmdArgs, "--version", version)
	}

	// Add --set values
	for k, v := range values {
		cmdArgs = append(cmdArgs, "--set", fmt.Sprintf("%s=%s", k, v))
	}

	// Add values YAML if specified
	if valuesYAML != "" {
		cmdArgs = append(cmdArgs, "--values", "-")
	}

	if wait {
		cmdArgs = append(cmdArgs, "--wait")
	}

	if timeout != "" {
		cmdArgs = append(cmdArgs, "--timeout", timeout)
	}

	if dryRun {
		cmdArgs = append(cmdArgs, "--dry-run")
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if valuesYAML != "" {
		cmd.Stdin = strings.NewReader(valuesYAML)
	}

	err := cmd.Run()

	if dryRun && err == nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "would-install",
			Message:     stdout.String(),
		}
	}

	if err != nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "failed",
			Message:     stderr.String(),
		}
	}

	// Determine if it was install or upgrade from output
	status := "installed"
	if strings.Contains(stdout.String(), "has been upgraded") {
		status = "upgraded"
	}

	return HelmResult{
		Cluster:     cluster,
		ReleaseName: releaseName,
		Namespace:   namespace,
		Status:      status,
		Message:     stdout.String(),
	}
}

// handleHelmUninstall uninstalls a Helm release from clusters
func (s *Server) handleHelmUninstall(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		ReleaseName string   `json:"release_name"`
		Namespace   string   `json:"namespace"`
		DryRun      bool     `json:"dry_run"`
		Clusters    []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ReleaseName == "" {
		return nil, fmt.Errorf("release_name is required")
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if err := server.ValidateNamespace(params.Namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate identifiers against Kubernetes naming rules to prevent flag injection (#269).
	if err := validateHelmIdentifier("release_name", params.ReleaseName); err != nil {
		return nil, err
	}
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return nil, err
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return nil, err
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		// Find clusters where release exists
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			if s.helmReleaseExists(ctx, c.Name, params.ReleaseName, params.Namespace) {
				targetClusters = append(targetClusters, c.Name)
			}
		}
	}

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("release %s not found in any cluster", params.ReleaseName)
	}

	var results []HelmResult
	for _, cluster := range targetClusters {
		result := s.helmUninstall(ctx, cluster, params.ReleaseName, params.Namespace, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "uninstalled" || r.Status == "would-uninstall" {
			successCount++
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
}

// helmUninstall runs helm uninstall for a single cluster
func (s *Server) helmUninstall(ctx context.Context, cluster, releaseName, namespace string, dryRun bool) HelmResult {
	if dryRun {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "would-uninstall",
			Message:     fmt.Sprintf("Would uninstall release %s from namespace %s", releaseName, namespace),
		}
	}

	cmdArgs := []string{"uninstall", releaseName,
		"--namespace", namespace,
		"--kube-context", cluster,
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "failed",
			Message:     stderr.String(),
		}
	}

	return HelmResult{
		Cluster:     cluster,
		ReleaseName: releaseName,
		Namespace:   namespace,
		Status:      "uninstalled",
		Message:     stdout.String(),
	}
}

// handleHelmList lists Helm releases across clusters
func (s *Server) handleHelmList(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Namespace string   `json:"namespace"`
		AllNs     bool     `json:"all_namespaces"`
		Filter    string   `json:"filter"`
		Clusters  []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return nil, err
	}

	// Validate namespace to prevent flag injection (#344).
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return nil, err
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if params.Namespace != "" {
		if err := server.ValidateNamespace(params.Namespace); err != nil {
			return nil, fmt.Errorf("invalid namespace: %w", err)
		}
	}

	// Validate filter to prevent flag injection (#344).
	if params.Filter != "" && strings.HasPrefix(params.Filter, "-") {
		return nil, fmt.Errorf("filter %q must not begin with '-' (possible flag injection)", params.Filter)
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			targetClusters = append(targetClusters, c.Name)
		}
	}

	allReleases := make(map[string][]HelmReleaseInfo)
	for _, cluster := range targetClusters {
		releases := s.helmList(ctx, cluster, params.Namespace, params.AllNs, params.Filter)
		if len(releases) > 0 {
			allReleases[cluster] = releases
		}
	}

	totalReleases := 0
	for _, releases := range allReleases {
		totalReleases += len(releases)
	}

	return map[string]interface{}{
		"clusters":      targetClusters,
		"releases":      allReleases,
		"totalReleases": totalReleases,
	}, nil
}

// helmList runs helm list for a single cluster
func (s *Server) helmList(ctx context.Context, cluster, namespace string, allNs bool, filter string) []HelmReleaseInfo {
	cmdArgs := []string{"list", "--kube-context", cluster, "-o", "json"}

	if allNs {
		cmdArgs = append(cmdArgs, "--all-namespaces")
	} else if namespace != "" {
		cmdArgs = append(cmdArgs, "--namespace", namespace)
	}

	if filter != "" {
		cmdArgs = append(cmdArgs, "--filter", filter)
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil
	}

	var releases []HelmReleaseInfo
	if err := json.Unmarshal(stdout.Bytes(), &releases); err != nil {
		return nil
	}
	return releases
}

// helmReleaseExists checks if a release exists in a cluster
func (s *Server) helmReleaseExists(ctx context.Context, cluster, releaseName, namespace string) bool {
	cmdArgs := []string{"status", releaseName,
		"--namespace", namespace,
		"--kube-context", cluster,
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	return cmd.Run() == nil
}

// handleHelmRollback rolls back a Helm release to a previous revision
func (s *Server) handleHelmRollback(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		ReleaseName string   `json:"release_name"`
		Namespace   string   `json:"namespace"`
		Revision    int      `json:"revision"`
		DryRun      bool     `json:"dry_run"`
		Clusters    []string `json:"clusters"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ReleaseName == "" {
		return nil, fmt.Errorf("release_name is required")
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	// Validate namespace to prevent access to system namespaces (#377).
	if err := server.ValidateNamespace(params.Namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}

	// Validate identifiers against Kubernetes naming rules to prevent flag injection (#269).
	if err := validateHelmIdentifier("release_name", params.ReleaseName); err != nil {
		return nil, err
	}
	if err := validateHelmIdentifier("namespace", params.Namespace); err != nil {
		return nil, err
	}

	// Validate user-supplied cluster names (#289).
	if err := validateHelmClusters(params.Clusters); err != nil {
		return nil, err
	}

	// Get target clusters
	targetClusters := params.Clusters
	if len(targetClusters) == 0 {
		clusters, err := s.manager.DiscoverClusters()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			if s.helmReleaseExists(ctx, c.Name, params.ReleaseName, params.Namespace) {
				targetClusters = append(targetClusters, c.Name)
			}
		}
	}

	if len(targetClusters) == 0 {
		return nil, fmt.Errorf("release %s not found in any cluster", params.ReleaseName)
	}

	var results []HelmResult
	for _, cluster := range targetClusters {
		result := s.helmRollback(ctx, cluster, params.ReleaseName, params.Namespace, params.Revision, params.DryRun)
		results = append(results, result)
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "rolled-back" || r.Status == "would-rollback" {
			successCount++
		}
	}

	return map[string]interface{}{
		"targetClusters": targetClusters,
		"successCount":   successCount,
		"totalClusters":  len(targetClusters),
		"results":        results,
		"dryRun":         params.DryRun,
	}, nil
}

// helmRollback runs helm rollback for a single cluster
func (s *Server) helmRollback(ctx context.Context, cluster, releaseName, namespace string, revision int, dryRun bool) HelmResult {
	cmdArgs := []string{"rollback", releaseName,
		"--namespace", namespace,
		"--kube-context", cluster,
	}

	if revision > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("%d", revision))
	}

	if dryRun {
		cmdArgs = append(cmdArgs, "--dry-run")
	}

	cmd := exec.CommandContext(ctx, "helm", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if dryRun && err == nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "would-rollback",
			Message:     stdout.String(),
		}
	}

	if err != nil {
		return HelmResult{
			Cluster:     cluster,
			ReleaseName: releaseName,
			Namespace:   namespace,
			Status:      "failed",
			Message:     stderr.String(),
		}
	}

	return HelmResult{
		Cluster:     cluster,
		ReleaseName: releaseName,
		Namespace:   namespace,
		Status:      "rolled-back",
		Message:     stdout.String(),
	}
}
