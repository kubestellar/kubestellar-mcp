package version

import "testing"

func TestVersionDefaults(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("Version = %q, want dev", Version)
	}
	if BuildDate != "unknown" {
		t.Fatalf("BuildDate = %q, want unknown", BuildDate)
	}
	if GitCommit != "unknown" {
		t.Fatalf("GitCommit = %q, want unknown", GitCommit)
	}
}

func TestVersionMetadataIsSet(t *testing.T) {
	if Version == "" || BuildDate == "" || GitCommit == "" {
		t.Fatalf("version metadata should never be empty: version=%q buildDate=%q gitCommit=%q", Version, BuildDate, GitCommit)
	}
}
