package main

import (
	"fmt"
	"os"

	"github.com/plainwork/boxx/cmd"
)

// version is injected at build time by goreleaser via -X main.version=<tag>
var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
