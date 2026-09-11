package extension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/validate"
)

// BundledBinaryName is the file name of the multi-call binary that provides
// every LocalStack-bundled extension. It ships next to lstk (in BundledDir)
// as one binary rather than one lstk-<name> copy per extension, and dispatches
// on the name it is invoked as: lstk execs it with argv[0] set to
// "lstk-<name>" (see Invoke), the busybox/git approach.
//
// This is what makes the bundle deliverable at all. Per-name copies would cost
// ~30 MB per extension in every archive and npm package; symlink aliases are
// dropped by the tar extractor, materialized as text files by the zip one, and
// need elevation to create on Windows. A single binary has none of those
// problems, at the cost of making the descriptions file load-bearing: it is the
// only record of which commands the binary provides, so LoadBundle treats a
// missing or unreadable one as a hard error rather than degrading to an empty
// set the way LoadDescriptions does for help rendering.
const BundledBinaryName = "bundled-extensions"

// Bundle is the installed multi-call bundle: the path to its binary and the
// commands it provides with their help descriptions, taken from the
// descriptions file.
type Bundle struct {
	Path         string
	descriptions map[string]string
}

// Provides reports whether the bundle provides the given extension command.
func (b *Bundle) Provides(name string) bool {
	_, ok := b.descriptions[name]
	return ok
}

// Names returns the bundle's extension command names, sorted.
func (b *Bundle) Names() []string {
	names := make([]string, 0, len(b.descriptions))
	for name := range b.descriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Description returns the one-line help description of a bundled command, or
// "" when the bundle does not provide it.
func (b *Bundle) Description(name string) string {
	return b.descriptions[name]
}

// LoadBundle returns the multi-call bundle installed in dir, or (nil, nil)
// when dir has no BundledBinaryName executable — the pre-bundling shape, and
// any user who has only lstk-<name> files there. When the binary IS present,
// the descriptions file becomes mandatory: a missing, unreadable, malformed,
// or empty one is returned as an error, because without it lstk cannot know
// which commands the binary answers to, and silently reporting "unknown
// command" would hide a broken install.
//
// Every key must also be a dispatchable command name (validate.ExtensionName,
// the rule the release gate applies to the same file). A key that fails it
// would make lstk exec the binary under an argv[0] nothing answers to, so it is
// reported as a broken bundle rather than skipped: the file is LocalStack's own
// release artifact, and an inconsistency in it is a release bug to surface, not
// a per-entry condition to tolerate.
func LoadBundle(dir string, logger log.Logger) (*Bundle, error) {
	if dir == "" {
		return nil, nil
	}
	path := findExecutable(dir, BundledBinaryName)
	if path == "" {
		return nil, nil
	}

	descPath := filepath.Join(dir, DescriptionsFileName)
	descriptions, err := readDescriptions(descPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("bundled extensions binary %s is installed but its command list %s is missing; reinstall lstk to restore it", path, descPath)
		}
		return nil, fmt.Errorf("bundled extensions command list %s is not usable: %w", descPath, err)
	}
	if len(descriptions) == 0 {
		return nil, fmt.Errorf("bundled extensions command list %s describes no commands", descPath)
	}
	for name := range descriptions {
		if err := validate.ExtensionName(name); err != nil {
			return nil, fmt.Errorf("bundled extensions command list %s has an invalid command name %q: %w", descPath, name, err)
		}
	}
	logger.Info("extension: bundle at %s provides %d command(s)", path, len(descriptions))
	return &Bundle{Path: path, descriptions: descriptions}, nil
}
