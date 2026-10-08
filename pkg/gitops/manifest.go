package gitops

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
)

// ManifestSource represents where to get manifests from
type ManifestSource struct {
	Repo   string // Git repository URL
	Path   string // Path within repo
	Branch string // Branch name (default: main)
}

// Manifest represents a parsed Kubernetes manifest
type Manifest struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   ManifestMetadata       `json:"metadata"`
	Spec       map[string]interface{} `json:"spec,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Raw        map[string]interface{} `json:"-"`
}

// ManifestMetadata contains metadata fields
type ManifestMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ResourceKey uniquely identifies a resource
type ResourceKey struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

func (k ResourceKey) String() string {
	if k.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", k.APIVersion, k.Kind, k.Namespace, k.Name)
	}
	return fmt.Sprintf("%s/%s/%s", k.APIVersion, k.Kind, k.Name)
}

// ManifestReader reads manifests from various sources
type ManifestReader struct {
	tempDir string
	// AllowedSchemes overrides the default URL scheme allowlist for validation.
	// When nil, the default safe set (https only) is used.
	// Tests that need local repos can set this to include "file".
	AllowedSchemes map[string]bool
}

// NewManifestReader creates a new manifest reader with default safe URL schemes
func NewManifestReader() *ManifestReader {
	return &ManifestReader{}
}

// NewManifestReaderWithSchemes creates a manifest reader with custom allowed URL schemes.
// This is intended for testing only — production code should use NewManifestReader().
func NewManifestReaderWithSchemes(schemes map[string]bool) *ManifestReader {
	return &ManifestReader{AllowedSchemes: schemes}
}

// ReadFromGit clones a repo and reads manifests.
// ctx is used to cancel the git clone subprocess if the caller's context is done.
// The repo URL is validated against the reader's allowed schemes (defaults to https/http).
func (r *ManifestReader) ReadFromGit(ctx context.Context, source ManifestSource) ([]Manifest, error) {
	// Validate repo URL to prevent SSRF and local file reads
	schemes := r.AllowedSchemes
	if schemes == nil {
		schemes = allowedRepoSchemes
	}
	if err := validateRepoURLWithSchemes(source.Repo, schemes); err != nil {
		return nil, fmt.Errorf("repo URL validation failed: %w", err)
	}

	if err := r.resetTempDir(); err != nil {
		return nil, err
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "kubestellar-deploy-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	r.tempDir = tempDir
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			r.Cleanup()
		}
	}()

	// Clone the repo
	branch := source.Branch
	if branch == "" {
		branch = "main"
	}
	if err := validateBranchName(branch); err != nil {
		return nil, err
	}

	// Narrow the TOCTOU window between validateRepoURLWithSchemes above and
	// git's own DNS resolution below: re-resolve the host immediately before
	// exec and re-apply the block list. This mirrors the helm mitigation
	// (see pkg/deploy/mcp/tools_helm.go: revalidateHelmHosts, issue #275)
	// and defends against DNS rebinding to cloud-metadata / RFC 1918 / CGNAT
	// addresses between validation and clone. See issue #884.
	if err := revalidateRepoHost(source.Repo); err != nil {
		return nil, fmt.Errorf("repo URL %q failed re-validation before clone: %w", source.Repo, err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", branch, "--", source.Repo, tempDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w\n%s", err, output)
	}

	manifestPath, err := resolveManifestPath(tempDir, source.Path)
	if err != nil {
		return nil, err
	}
	manifests, err := r.ReadFromPath(manifestPath)
	if err != nil {
		return nil, err
	}
	cleanupOnError = false
	return manifests, nil
}

// ReadFromPath reads all YAML manifests from a directory.
//
// Symbolic links are skipped: an attacker-controlled git repo can commit
// a symlink such as `leak.yaml -> /var/run/secrets/kubernetes.io/serviceaccount/token`,
// and `os.Open` would then follow the target and load host-side file content
// into the returned Manifest (surfacing it via drift-detect responses).
// See issue #945.
func (r *ManifestReader) ReadFromPath(path string) ([]Manifest, error) {
	var manifests []Manifest

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip symbolic links: filepath.Walk uses Lstat (so this Mode
		// reliably reflects the entry itself, not its target), and we
		// must not follow a symlink out of the cloned tree. Any real
		// manifest file in the repo will be a regular file.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Only process YAML files
		ext := strings.ToLower(filepath.Ext(filePath))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		fileManifests, err := r.ReadFromFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filePath, err)
		}

		manifests = append(manifests, fileManifests...)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return manifests, nil
}

// ReadFromFile reads manifests from a single file
func (r *ManifestReader) ReadFromFile(filePath string) ([]Manifest, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	return r.ReadFromReader(file)
}

// ReadFromReader reads manifests from an io.Reader
func (r *ManifestReader) ReadFromReader(reader io.Reader) ([]Manifest, error) {
	var manifests []Manifest

	decoder := yaml.NewYAMLOrJSONDecoder(reader, 4096)

	for {
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if raw == nil {
			continue
		}

		manifest := parseManifest(raw)
		if manifest.Kind != "" {
			manifests = append(manifests, manifest)
		}
	}

	return manifests, nil
}

func resolveManifestPath(baseDir, requestedPath string) (string, error) {
	if filepath.IsAbs(requestedPath) {
		return "", fmt.Errorf("invalid path %q escapes repository directory", requestedPath)
	}
	resolvedPath := filepath.Clean(filepath.Join(baseDir, requestedPath))
	rel, err := filepath.Rel(baseDir, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", requestedPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path %q escapes repository directory", requestedPath)
	}
	return resolvedPath, nil
}

func (r *ManifestReader) resetTempDir() error {
	if r.tempDir == "" {
		return nil
	}
	if err := os.RemoveAll(r.tempDir); err != nil {
		return fmt.Errorf("failed to remove previous temp dir %q: %w", r.tempDir, err)
	}
	r.tempDir = ""
	return nil
}

// Cleanup removes temporary files. A failure here means a clone's temp
// directory (which may hold GitOps manifests fetched from the configured
// repo) is leaked on disk - it was previously silently discarded with no
// signal at all, unlike every other error path in this package, which logs
// via klog (see sync.go/drift.go). Logging it here closes that gap without
// changing Cleanup's void signature, since all call sites
// (pkg/deploy/mcp/gitops/gitops.go, pkg/mcp/server/drift/drift.go) invoke
// it via `defer reader.Cleanup()` and do not expect an error return.
func (r *ManifestReader) Cleanup() {
	if err := r.resetTempDir(); err != nil {
		klog.ErrorS(err, "gitops manifest reader cleanup failed", "tempDir", r.tempDir)
	}
}

// parseManifest parses a raw map into a Manifest
func parseManifest(raw map[string]interface{}) Manifest {
	m := Manifest{Raw: raw}

	if v, ok := raw["apiVersion"].(string); ok {
		m.APIVersion = v
	}
	if v, ok := raw["kind"].(string); ok {
		m.Kind = v
	}

	if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
		if v, ok := metadata["name"].(string); ok {
			m.Metadata.Name = v
		}
		if v, ok := metadata["namespace"].(string); ok {
			m.Metadata.Namespace = v
		}
		if v, ok := metadata["labels"].(map[string]interface{}); ok {
			m.Metadata.Labels = make(map[string]string)
			for k, val := range v {
				if s, ok := val.(string); ok {
					m.Metadata.Labels[k] = s
				}
			}
		}
		if v, ok := metadata["annotations"].(map[string]interface{}); ok {
			m.Metadata.Annotations = make(map[string]string)
			for k, val := range v {
				if s, ok := val.(string); ok {
					m.Metadata.Annotations[k] = s
				}
			}
		}
	}

	if spec, ok := raw["spec"].(map[string]interface{}); ok {
		m.Spec = spec
	}
	if data, ok := raw["data"].(map[string]interface{}); ok {
		m.Data = data
	}

	return m
}

// GetKey returns the unique key for a manifest
func (m *Manifest) GetKey() ResourceKey {
	return ResourceKey{
		APIVersion: m.APIVersion,
		Kind:       m.Kind,
		Namespace:  m.Metadata.Namespace,
		Name:       m.Metadata.Name,
	}
}

// GetNamespace returns the namespace, defaulting to "default" if empty
func (m *Manifest) GetNamespace() string {
	if m.Metadata.Namespace == "" {
		return "default"
	}
	return m.Metadata.Namespace
}
