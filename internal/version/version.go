package version

// Set via ldflags at build time. Must be a variable, not a constant,
// because the linker can only modify variables at link time.
var version = "dev"

// bundlesExtensions is "true" on release builds, which ship the
// bundled-extensions binary and lstk-extensions.toml beside lstk.
var bundlesExtensions = "false"

func Version() string { return version }

// BundlesExtensions reports whether this build expects a bundle beside it.
func BundlesExtensions() bool { return bundlesExtensions == "true" }
