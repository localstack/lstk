package extension

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/log"
)

func TestLoadDescriptionsReadsFile(t *testing.T) {
	dir := t.TempDir()
	body := "deploy = \"Deploy your application to LocalStack\"\nbackup = \"Back up emulator state\"\n"
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadDescriptions(dir, log.Nop())
	if got["deploy"] != "Deploy your application to LocalStack" {
		t.Errorf("deploy description = %q", got["deploy"])
	}
	if got["backup"] != "Back up emulator state" {
		t.Errorf("backup description = %q", got["backup"])
	}
}

func TestLoadDescriptionsMissingFileDegrades(t *testing.T) {
	got := LoadDescriptions(t.TempDir(), log.Nop())
	if len(got) != 0 {
		t.Fatalf("expected empty map for missing file, got %+v", got)
	}
}

func TestLoadDescriptionsEmptyDir(t *testing.T) {
	if got := LoadDescriptions("", log.Nop()); len(got) != 0 {
		t.Fatalf("expected empty map for empty dir, got %+v", got)
	}
}

func TestLoadDescriptionsMalformedDegrades(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte("this is not = valid = toml ="), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadDescriptions(dir, log.Nop()); len(got) != 0 {
		t.Fatalf("expected empty map for malformed file, got %+v", got)
	}
}
