package buildinfo

// These values are injected at build time by scripts/build-daz.sh via -ldflags.
// Defaults are preserved for local/test builds that do not inject metadata.
var (
	GitCommit = "unknown"
	GitDirty  = "unknown"
)
