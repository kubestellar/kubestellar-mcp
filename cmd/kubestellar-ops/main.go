package main

import (
	"os"

	"github.com/kubestellar/kubestellar-mcp/pkg/cmd"
)

var (
	execute = cmd.Execute
	exit    = os.Exit
)

func main() {
	if err := execute(); err != nil {
		exit(1)
	}
}
