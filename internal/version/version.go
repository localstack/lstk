package version

// Set via ldflags at build time. Must be variables, not constants,
// because the linker can only modify variables at link time.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func Version() string { return version }

// Keep linker-populated build metadata embedded for release tooling,
// even though user-facing version output only exposes the version string.
func metadata() (string, string) {
	return commit, buildDate
}
