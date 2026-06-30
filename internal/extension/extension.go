// Package extension implements lstk's Git-style extension mechanism: when a user
// runs `lstk <name>` and `<name>` is not a built-in command, lstk resolves and
// executes an external `lstk-<name>` executable, forwarding arguments, streams,
// and the exit code. This mirrors Git's `git-<name>` model and lstk's own IaC
// proxies, and is the only model that cleanly supports closed-source and
// third-party extensions written in any language: an extension is an opaque
// binary that never touches the core repository.
package extension

// APIVersion is the integer version of the LSTK_EXT_* runtime-context contract
// that this lstk implements. It is exposed to extensions as
// LSTK_EXT_API_VERSION. Bump it only when a variable is removed or repurposed;
// adding a new variable is an additive change that keeps the same version.
const APIVersion = 1

// NamePrefix is the executable-name prefix that identifies an extension: an
// executable named "lstk-<name>" provides the "<name>" extension.
const NamePrefix = "lstk-"

// Extension is a resolved extension executable: its command name (the part after
// the "lstk-" prefix) and the absolute path to the executable that provides it.
// Bundled reports whether it was resolved from the bundled-extensions directory
// (which ships with lstk and takes precedence over PATH) rather than from PATH.
type Extension struct {
	Name    string
	Path    string
	Bundled bool
}

// NewExtension returns an Extension for the given command name and executable path.
func NewExtension(name, path string, bundled bool) *Extension {
	return &Extension{Name: name, Path: path, Bundled: bundled}
}
