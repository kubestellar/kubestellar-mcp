package helm

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateClusters_Exported exercises the exported ValidateClusters
// wrapper so the pkg/deploy/mcp/helm sub-package's own coverage credits the
// SSRF/flag-injection guard on cluster names. The wrapper is a thin
// pass-through to validateHelmClusters; cross-package callers (see
// pkg/deploy/mcp/kustomize_adapter.go) do not raise coverage of this file.
func TestValidateClusters_Exported(t *testing.T) {
	tests := []struct {
		name     string
		clusters []string
		wantErr  bool
	}{
		{name: "empty list allowed", clusters: nil, wantErr: false},
		{name: "valid names accepted", clusters: []string{"alpha", "beta-1"}, wantErr: false},
		{name: "flag injection rejected", clusters: []string{"--kubeconfig=/etc/passwd"}, wantErr: true},
		{name: "uppercase rejected", clusters: []string{"Alpha"}, wantErr: true},
		{name: "leading dash rejected", clusters: []string{"-alpha"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateClusters(tc.clusters)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateClusters(%v) error = %v, wantErr %v", tc.clusters, err, tc.wantErr)
			}
		})
	}
}

// TestSetHostResolver_ReplaceAndRestore verifies that SetHostResolver both
// installs the supplied resolver AND returns a restore func that puts the
// prior resolver back — matching the contract relied on by
// pkg/deploy/mcp/resolve_and_block_test.go.
func TestSetHostResolver_ReplaceAndRestore(t *testing.T) {
	orig := helmHostResolver
	t.Cleanup(func() { helmHostResolver = orig })

	sentinel := errors.New("stub-resolver-invoked")
	stub := func(_ string) ([]string, error) { return nil, sentinel }

	restore := SetHostResolver(stub)
	if _, err := helmHostResolver("example.com"); !errors.Is(err, sentinel) {
		t.Fatalf("after SetHostResolver, resolver err = %v, want sentinel", err)
	}

	restore()
	// The restored resolver must not be the stub anymore. Compare by calling
	// it against a well-known IP literal (net.DefaultResolver.LookupHost
	// returns the input for IP literals), and expect no sentinel error.
	if _, err := helmHostResolver("127.0.0.1"); errors.Is(err, sentinel) {
		t.Fatalf("restore() did not reinstall the prior resolver; stub still active")
	}
}

// TestResolveAndBlock_ExportedDelegates confirms the exported ResolveAndBlock
// wrapper delegates to the internal resolveAndBlock — a blocked private IP
// literal must still be rejected when called through the exported entrypoint.
func TestResolveAndBlock_ExportedDelegates(t *testing.T) {
	orig := helmHostResolver
	t.Cleanup(func() { helmHostResolver = orig })
	// Resolver should never be consulted for a literal IP host.
	helmHostResolver = func(host string) ([]string, error) {
		t.Errorf("unexpected DNS lookup for literal IP host %q", host)
		return nil, nil
	}

	if err := ResolveAndBlock("10.0.0.1"); err == nil {
		t.Fatalf("ResolveAndBlock(private IP) error = nil, want blocked-IP error")
	}
	if err := ResolveAndBlock("93.184.216.34"); err != nil {
		t.Fatalf("ResolveAndBlock(public IP) error = %v, want nil", err)
	}
}

// TestServer_Tools_ShapeAndOrder verifies Tools() returns the four helm
// tools in their pre-refactor registration order with the required-args
// schema each caller depends on. Order matters because tools/list output
// must stay byte-identical across the epic-#983 refactor (see the doc
// comment in register.go on Tools).
func TestServer_Tools_ShapeAndOrder(t *testing.T) {
	s := &Server{}
	tools := s.Tools()

	wantOrder := []string{"helm_install", "helm_uninstall", "helm_list", "helm_rollback"}
	if len(tools) != len(wantOrder) {
		t.Fatalf("Tools() returned %d entries, want %d", len(tools), len(wantOrder))
	}
	for i, want := range wantOrder {
		if tools[i].Name != want {
			t.Errorf("Tools()[%d].Name = %q, want %q", i, tools[i].Name, want)
		}
		if tools[i].Handler == nil {
			t.Errorf("Tools()[%d] (%s) has nil Handler", i, tools[i].Name)
		}
		if tools[i].Description == "" {
			t.Errorf("Tools()[%d] (%s) has empty Description", i, tools[i].Name)
		}
		if tools[i].InputSchema == nil {
			t.Fatalf("Tools()[%d] (%s) has nil InputSchema", i, tools[i].Name)
		}
		if got := tools[i].InputSchema["type"]; got != "object" {
			t.Errorf("Tools()[%d] (%s) InputSchema.type = %v, want \"object\"", i, tools[i].Name, got)
		}
	}

	// helm_install and helm_uninstall/rollback all require release_name.
	// helm_list has no required fields.
	wantRequired := map[string][]string{
		"helm_install":   {"release_name", "chart"},
		"helm_uninstall": {"release_name"},
		"helm_rollback":  {"release_name"},
	}
	for _, tool := range tools {
		want, ok := wantRequired[tool.Name]
		if !ok {
			continue
		}
		gotRaw, ok := tool.InputSchema["required"]
		if !ok {
			t.Errorf("%s InputSchema missing required field", tool.Name)
			continue
		}
		got, ok := gotRaw.([]string)
		if !ok {
			t.Errorf("%s InputSchema.required has type %T, want []string", tool.Name, gotRaw)
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s required = %v, want %v", tool.Name, got, want)
		}
	}
}
