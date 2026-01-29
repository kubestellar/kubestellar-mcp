package main

import (
	"os"

	deploycmd "github.com/kubestellar/kubestellar-mcp/pkg/deploy/cmd"
)

func main() {
	if err := deploycmd.Execute(); err != nil {
		os.Exit(1)
	}
}
