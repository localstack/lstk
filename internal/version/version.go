package version

// Set via ldflags at build time. Must be variables, not constants,
// because the linker can only modify variables at link time.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func Version() string { return version }
