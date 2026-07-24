package gitops

import (
	"context"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

// TestNewDriftDetector verifies the constructor returns a fully-wired
// detector for a well-formed *rest.Config. Previously 0% covered.
func TestNewDriftDetector(t *testing.T) {
	dd, err := NewDriftDetector(&rest.Config{Host: "https://kube.example.invalid"})
	if err != nil {
		t.Fatalf("NewDriftDetector returned error: %v", err)
	}
	if dd == nil {
		t.Fatal("NewDriftDetector returned nil detector")
	}
	if dd.client == nil {
		t.Error("expected non-nil kubernetes clientset")
	}
	if dd.dynClient == nil {
		t.Error("expected non-nil dynamic client")
	}
	// restMapper may be nil if discovery could not reach the cluster;
	// newRESTMapper is documented to degrade gracefully.
}

// TestNewDriftDetector_InvalidConfig ensures an unusable rest.Config
// surfaces an error rather than panicking.
func TestNewDriftDetector_InvalidConfig(t *testing.T) {
	// A config with an unparseable host triggers NewForConfig failure.
	_, err := NewDriftDetector(&rest.Config{Host: "://not a url"})
	if err == nil {
		t.Fatal("expected error from malformed rest.Config, got nil")
	}
}

// TestNewSyncer verifies the constructor returns a fully-wired syncer.
// Previously 0% covered.
func TestNewSyncer(t *testing.T) {
	s, err := NewSyncer(&rest.Config{Host: "https://kube.example.invalid"})
	if err != nil {
		t.Fatalf("NewSyncer returned error: %v", err)
	}
	if s == nil {
		t.Fatal("NewSyncer returned nil syncer")
	}
	if s.dynClient == nil {
		t.Error("expected non-nil dynamic client")
	}
}

func TestNewSyncer_InvalidConfig(t *testing.T) {
	_, err := NewSyncer(&rest.Config{Host: "://not a url"})
	if err == nil {
		t.Fatal("expected error from malformed rest.Config, got nil")
	}
}

// TestNewRESTMapper_DegradesGracefully exercises the newRESTMapper fallback
// path. When discovery cannot be reached, the function must return nil
// (never panic) so callers fall back to static mapping. Previously 0% covered.
func TestNewRESTMapper_DegradesGracefully(t *testing.T) {
	// A malformed host makes discovery.NewDiscoveryClientForConfig fail;
	// even if construction succeeds, GetAPIGroupResources will fail
	// against a non-existent host. Either path returns nil.
	m := newRESTMapper(&rest.Config{Host: "://malformed"})
	if m != nil {
		t.Errorf("expected nil RESTMapper on discovery failure, got %#v", m)
	}
}

// TestReadFromGit_RejectsInvalidScheme confirms URL-scheme validation
// runs before any git subprocess is spawned. Previously ReadFromGit was
// 0% covered.
func TestReadFromGit_RejectsInvalidScheme(t *testing.T) {
	r := NewManifestReader()
	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   "file:///etc/passwd",
		Branch: "main",
	})
	if err == nil {
		t.Fatal("expected scheme validation error, got nil")
	}
	if !strings.Contains(err.Error(), "repo URL validation failed") {
		t.Errorf("expected repo URL validation error, got: %v", err)
	}
}

func TestReadFromGit_RejectsEmptyRepo(t *testing.T) {
	r := NewManifestReader()
	_, err := r.ReadFromGit(context.Background(), ManifestSource{Repo: ""})
	if err == nil {
		t.Fatal("expected error for empty repo, got nil")
	}
}

func TestReadFromGit_RejectsInvalidBranch(t *testing.T) {
	// Use custom schemes so URL validation passes and we exercise the
	// branch-name validator before any subprocess launch.
	r := NewManifestReaderWithSchemes(map[string]bool{"https": true})
	_, err := r.ReadFromGit(context.Background(), ManifestSource{
		Repo:   "https://example.invalid/repo.git",
		Branch: "bad;branch$name", // contains characters outside [a-zA-Z0-9._/-]
	})
	if err == nil {
		t.Fatal("expected branch validation error, got nil")
	}
}
