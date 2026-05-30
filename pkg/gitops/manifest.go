package gitops

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// allowedRepoSchemes restricts git clone to safe URL schemes.
// file:// and ssh:// are blocked to prevent SSRF and local file reads.
var allowedRepoSchemes = map[string]bool{
	"https": true,
	"http":  true,
}

// ValidateRepoURL ensures the repository URL uses an allowed scheme
// to prevent SSRF, local file reads, and arbitrary SSH connections.
func ValidateRepoURL(repo string) error {
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
	if !allowedRepoSchemes[u.Scheme] {
		return fmt.Errorf("repo URL scheme %q is not allowed; use https://", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("repo URL must include a host; got %q", repo)
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
}

// NewManifestReader creates a new manifest reader
func NewManifestReader() *ManifestReader {
	return &ManifestReader{}
}

// ReadFromGit clones a repo and reads manifests.
// ctx is used to cancel the git clone subprocess if the caller's context is done.
// The repo URL is validated to only allow https:// and http:// schemes.
func (r *ManifestReader) ReadFromGit(ctx context.Context, source ManifestSource) ([]Manifest, error) {
	// Validate repo URL to prevent SSRF and local file reads
	if err := ValidateRepoURL(source.Repo); err != nil {
		return nil, fmt.Errorf("repo URL validation failed: %w", err)
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "kubestellar-deploy-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	r.tempDir = tempDir

	// Clone the repo
	branch := source.Branch
	if branch == "" {
		branch = "main"
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", branch, source.Repo, tempDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to clone repo: %w\n%s", err, output)
	}

	// Read manifests from path
	manifestPath := filepath.Join(tempDir, source.Path)
	return r.ReadFromPath(manifestPath)
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
	defer file.Close()

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

// Cleanup removes temporary files
func (r *ManifestReader) Cleanup() {
	if r.tempDir != "" {
		os.RemoveAll(r.tempDir)
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
