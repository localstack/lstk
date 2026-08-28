// Package extension implements lstk's Git-style extension mechanism: when a user
// runs `lstk <name>` and `<name>` is not a built-in command, lstk resolves and
// executes an external `lstk-<name>` executable, forwarding arguments, streams,
// and the exit code. This mirrors Git's `git-<name>` model and lstk's own IaC
// proxies, and is the only model that cleanly supports closed-source and
// third-party extensions written in any language: an extension is an opaque
// binary that never touches the core repository.
package extension

import "path/filepath"

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
//
// Argv0 is the name the executable is invoked as (its argv[0]). For a
// standalone lstk-<name> file it is simply the file's own base name; for a
// command provided by the multi-call bundle (BundledBinaryName) it is
// "lstk-<name>", which is how that one binary learns which extension to be.
//
// Description is the one-line help text for a bundled extension, taken from the
// descriptions file beside it. It is empty for PATH extensions (help lists them
// name-only) and for bundled files the file does not describe. Resolver.List
// fills it in; Resolve leaves it empty, since dispatch never renders it.
type Extension struct {
	Name        string
	Path        string
	Bundled     bool
	Argv0       string
	Description string
}

// NewExtension returns an Extension for the given command name and executable
// path, invoked under its own file name.
func NewExtension(name, path string, bundled bool) *Extension {
	return &Extension{Name: name, Path: path, Bundled: bundled, Argv0: filepath.Base(path)}
}
