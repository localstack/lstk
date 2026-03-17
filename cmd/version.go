package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/version"
)

func versionLine() string {
	return fmt.Sprintf("lstk %s", version.Version())
}
