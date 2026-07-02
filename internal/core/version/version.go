package version

// value is overridden at build time via -ldflags "-X ...version.value=..."
var value = "dev"

// Version returns the build version string.
func Version() string { return value }
