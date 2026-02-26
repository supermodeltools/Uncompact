package main

import (
	"os"

	"github.com/supermodeltools/uncompact/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
