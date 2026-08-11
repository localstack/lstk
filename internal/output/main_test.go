package output

import (
	"os"
	"testing"

	"github.com/localstack/lstk/internal/snap"
)

// TestMain wires snapshot cleanup: snapshots no test uses anymore fail the
// run (or are deleted with UPDATE_SNAPS=true).
func TestMain(m *testing.M) {
	os.Exit(snap.Clean(m))
}
