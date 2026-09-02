package version

import "strings"

// Set via ldflags at build time. Must be a variable, not a constant,
// because the linker can only modify variables at link time.
var version = "dev"

func Version() string { return version }

// bundledSet is set via ldflags at build time to the comma-separated
// archive-root names of the bundled-extension files this release ships — the
// multi-call extensions binary and the descriptions file, with ".exe" included
// on Windows builds (e.g. "bundled-extensions,lstk-extensions.toml").
//
// It is empty on every build that ships no bundle: every release before
// bundled extensions existed, and any release deliberately built without one.
// An empty value keeps `lstk update` on a pure version comparison, which is
// what makes a rollback to an extension-free release behave exactly as it did
// before bundling.
//
// It has to be stamped in rather than derived from disk because a bundling
// release is reachable by pre-bundling updaters, which install only the lstk
// binary and ignore the archive's other members. The install directory they
// leave behind cannot testify to what it should contain, so this is the only
// record the running binary has of the set its own release shipped.
var bundledSet = ""

// BundledSet returns the archive-root names of the bundled-extension files
// this release ships, or nil when it ships none.
func BundledSet() []string {
	var names []string
	for _, name := range strings.Split(bundledSet, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}
