package main

import (
	"os"

	deploycmd "github.com/kubestellar/klaude/pkg/deploy/cmd"
)

func main() {
	if err := deploycmd.Execute(); err != nil {
		os.Exit(1)
	}
}
