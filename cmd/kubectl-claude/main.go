package main

import (
	"os"

	"github.com/kubestellar/kubectl-claude/pkg/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
