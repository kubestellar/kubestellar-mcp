package handlers

import (
	"context"
	"testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

func TestRegistry_RegisterFindTools(t *testing.T) {
	r := NewRegistry()
	if got := r.Find("missing"); got != nil {
		t.Fatal("Find on empty registry should return nil")
	}
	if got := r.Tools(); len(got) != 0 {
		t.Fatalf("Tools on empty registry = %d entries, want 0", len(got))
	}

	var called *Deps
	r.Register(protocol.Tool{Name: "alpha"}, func(_ context.Context, d *Deps, _ map[string]interface{}) (string, bool) {
		called = d
		return "a", false
	})
	r.Register(protocol.Tool{Name: "beta"}, func(context.Context, *Deps, map[string]interface{}) (string, bool) {
		return "b", true
	})

	tools := r.Tools()
	if len(tools) != 2 || tools[0].Name != "alpha" || tools[1].Name != "beta" {
		t.Fatalf("Tools() = %v, want [alpha beta] in registration order", tools)
	}

	deps := &Deps{Kubeconfig: "kc"}
	h := r.Find("alpha")
	if h == nil {
		t.Fatal("Find(alpha) returned nil")
	}
	if res, isErr := h(context.Background(), deps, nil); res != "a" || isErr {
		t.Fatalf("alpha handler = (%q, %v), want (a, false)", res, isErr)
	}
	if called != deps {
		t.Fatal("handler did not receive the Deps passed to it")
	}
	if res, isErr := r.Find("beta")(context.Background(), deps, nil); res != "b" || !isErr {
		t.Fatalf("beta handler = (%q, %v), want (b, true)", res, isErr)
	}
	if r.Find("gamma") != nil {
		t.Fatal("Find(gamma) should return nil")
	}
}

func TestRegistry_DefsReturnsCopy(t *testing.T) {
	r := NewRegistry()
	r.Register(protocol.Tool{Name: "alpha"}, nil)

	defs := r.Defs()
	if len(defs) != 1 || defs[0].Schema.Name != "alpha" {
		t.Fatalf("Defs() = %v, want single alpha entry", defs)
	}
	defs[0].Schema.Name = "mutated"
	if r.Tools()[0].Name != "alpha" {
		t.Fatal("mutating Defs() result must not affect the registry")
	}
}
