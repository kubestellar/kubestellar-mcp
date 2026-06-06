package gitops

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// allowedRepoSchemes restricts git clone to safe URL schemes.
// file://, ssh://, and http:// are blocked to prevent SSRF, local file reads,
// and man-in-the-middle attacks on fetched manifests.
var allowedRepoSchemes = map[string]bool{
	"https": true,
}

var validGitBranchPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

// ValidateRepoURL ensures the repository URL uses an allowed scheme
// to prevent SSRF, local file reads, and arbitrary SSH connections.
func ValidateRepoURL(repo string) error {
	return validateRepoURLWithSchemes(repo, allowedRepoSchemes)
}

// validateRepoURLWithSchemes validates a repo URL against a custom scheme allowlist.
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
		return fmt.Errorf("repo URL scheme %q is not allowed; use https://", u.Scheme)
	}
	// file:// URLs don't have a host — skip host check for file scheme
	if u.Scheme != "file" && u.Host == "" {
		return fmt.Errorf("repo URL must include a host; got %q", repo)
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
	// When nil, the default safe set (https) is used.
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
// The repo URL is validated against the reader's allowed schemes (defaults to https).
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

// ReadFromPath reads all YAML manifests from a directory
func (r *ManifestReader) ReadFromPath(path string) ([]Manifest, error) {
	var manifests []Manifest

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
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

// Cleanup removes temporary files
func (r *ManifestReader) Cleanup() {
	if err := r.resetTempDir(); err != nil {
		_ = err
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
