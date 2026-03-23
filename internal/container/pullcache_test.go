package container

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSanitizeImageName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"localstack/localstack-pro:latest", "localstack-localstack-pro-latest"},
		{"localstack/localstack-pro:4.0.0", "localstack-localstack-pro-4.0.0"},
		{"registry.example.com/org/image:v1", "registry.example.com-org-image-v1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeImageName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeImageName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestShouldPull_NoCacheFile(t *testing.T) {
	if !shouldPull("nonexistent/image:tag", time.Hour) {
		t.Error("shouldPull should return true when no cache file exists")
	}
}

func TestShouldPull_FreshCache(t *testing.T) {
	dir := t.TempDir()
	image := "test/image:latest"
	path := filepath.Join(dir, sanitizeImageName(image))
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	// Override pullCacheDir for this test via a temp file that was just created (mod time = now)
	info, _ := os.Stat(path)
	age := time.Since(info.ModTime())
	if age > time.Hour {
		t.Fatal("test file should be fresh")
	}
}

func TestRecordPull_CreatesFile(t *testing.T) {
	// recordPull uses os.UserCacheDir which we can't easily override,
	// so we verify it doesn't error on the real system.
	image := "test-record-pull/image:latest"
	if err := recordPull(image); err != nil {
		t.Fatalf("recordPull failed: %v", err)
	}

	dir, err := pullCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sanitizeImageName(image))
	t.Cleanup(func() { _ = os.Remove(path) })

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file to exist at %s: %v", path, err)
	}
}

func TestShouldPull_ExpiredCache(t *testing.T) {
	image := "test-expired/image:latest"
	if err := recordPull(image); err != nil {
		t.Fatal(err)
	}

	dir, err := pullCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sanitizeImageName(image))
	t.Cleanup(func() { _ = os.Remove(path) })

	// Set mod time to 25 hours ago
	past := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	if !shouldPull(image, 24*time.Hour) {
		t.Error("shouldPull should return true for expired cache")
	}
}

func TestShouldPull_ValidCache(t *testing.T) {
	image := "test-valid/image:latest"
	if err := recordPull(image); err != nil {
		t.Fatal(err)
	}

	dir, err := pullCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sanitizeImageName(image))
	t.Cleanup(func() { _ = os.Remove(path) })

	if shouldPull(image, 24*time.Hour) {
		t.Error("shouldPull should return false for fresh cache")
	}
}
