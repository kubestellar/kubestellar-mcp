package main

import (
	"os"

	deploycmd "github.com/kubestellar/kubestellar-mcp/pkg/deploy/cmd"
)

var (
	execute = deploycmd.Execute
	exit    = os.Exit
)

func main() {
	if err := execute(); err != nil {
		exit(1)
	}
}
