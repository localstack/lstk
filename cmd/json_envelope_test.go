package cmd

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/localstack/lstk/internal/output"
)

func TestClassifyConfigError_NotExistMapsToConfigNotFound(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("failed to read config file: %w", &os.PathError{Op: "open", Path: "/does/not/exist.toml", Err: os.ErrNotExist})
	envErr := classifyConfigError(err)

	if envErr.Code != output.ErrConfigNotFound {
		t.Fatalf("expected code %q, got %q", output.ErrConfigNotFound, envErr.Code)
	}
	if envErr.Category != output.CategoryConfig {
		t.Fatalf("expected category %q, got %q", output.CategoryConfig, envErr.Category)
	}
}

func TestClassifyConfigError_OtherErrorsMapToConfigInvalid(t *testing.T) {
	t.Parallel()

	err := errors.New("failed to read config file: unexpected token")
	envErr := classifyConfigError(err)

	if envErr.Code != output.ErrConfigInvalid {
		t.Fatalf("expected code %q, got %q", output.ErrConfigInvalid, envErr.Code)
	}
	if envErr.Category != output.CategoryConfig {
		t.Fatalf("expected category %q, got %q", output.CategoryConfig, envErr.Category)
	}
	if envErr.Message != err.Error() {
		t.Fatalf("expected message %q, got %q", err.Error(), envErr.Message)
	}
}
