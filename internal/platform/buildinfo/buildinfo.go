// Package buildinfo holds the version stamped at build time with
// -ldflags "-X github.com/lucabodd/obrabi/internal/platform/buildinfo.Version=...".
package buildinfo

// Version of the running binaries ("dev" for local builds).
var Version = "dev"
