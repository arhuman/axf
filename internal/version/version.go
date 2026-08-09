// Package version carries build metadata injected at link time.
package version

import "fmt"

// Build metadata, overridden via -ldflags -X at build time. The defaults are
// what a plain `go build` or `go run` produces.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// String renders the full build stamp on one line.
func String() string {
	return fmt.Sprintf("axf %s (commit %s, built %s)", Version, GitCommit, BuildDate)
}
