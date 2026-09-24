// Package versioncmd provides a shared cobra "version" subcommand used by
// every kubestellar-mcp CLI binary. Extracted from pkg/cmd/root.go and
// pkg/deploy/cmd/root.go, where two divergent implementations of the same
// subcommand had drifted: kubestellar-ops printed the ldflags-injected
// version + build date + git commit, while kubestellar-deploy printed a
// hardcoded "dev" — even in release builds, where the Makefile injects
// internal/version.Version via -ldflags. See the architect finding on
// kubestellar/kubestellar-mcp for the coupling analysis.
//
// This subpackage lives under internal/version/ (rather than in
// internal/version itself) so that pkg/deploy/mcp/server.go and other
// non-CLI consumers of internal/version can keep importing the plain
// version vars without pulling in a cobra dependency.
package versioncmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kubestellar/kubestellar-mcp/internal/version"
)

// New returns a cobra "version" subcommand that prints the ldflags-injected
// version, build date, and git commit for the given binary name. The output
// format matches the historical kubestellar-ops layout so downstream tooling
// that greps for "<bin> version" continues to work.
func New(binName string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s version %s\n", binName, version.Version)
			fmt.Printf("  Build date: %s\n", version.BuildDate)
			fmt.Printf("  Git commit: %s\n", version.GitCommit)
		},
	}
}
