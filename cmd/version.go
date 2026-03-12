package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/version"
)

func versionTemplate() string {
	return versionLine() + "\n"
}

func versionLine() string {
	return fmt.Sprintf("lstk %s", version.Version())
}
