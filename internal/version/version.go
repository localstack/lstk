package version

// Set via ldflags at build time. Must be a variable, not a constant,
// because the linker can only modify variables at link time.
var version = "dev"

func Version() string { return version }
