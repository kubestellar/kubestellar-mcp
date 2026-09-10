package mcp

import (
	"os"
	"testing"
)

// TestRunMCPServer_SuccessPath covers the previously-uncovered happy-path
// return statement of RunMCPServer (server.go:86) — i.e. `return server.Run()`
// once NewServer succeeds. The sibling
// TestRunMCPServer_PropagatesConstructionError already covers the failure
// branch, but the success branch was 0% because the code path reads
// os.Stdin. This test redirects os.Stdin to a pipe with the write side
// closed so scanner.Scan() sees EOF immediately, driving Run() through
// its happy exit without invoking any tool handlers.
//
// Lifts RunMCPServer from 75.0% to 100% statement coverage.
func TestRunMCPServer_SuccessPath(t *testing.T) {
	kubeconfig := writeMinimalKubeconfig(t)
	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("HOME", t.TempDir())

	// Redirect os.Stdin to an already-closed pipe so RunMCPServer's
	// bufio scanner sees EOF immediately and Run() returns nil.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})

	if err := RunMCPServer(); err != nil {
		t.Fatalf("RunMCPServer() on EOF stdin: %v, want nil", err)
	}
}
