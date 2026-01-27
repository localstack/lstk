package main

import (
	"os"

	"github.com/localstack/lstk/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
