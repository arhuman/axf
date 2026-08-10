package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Launcher starts host processes on behalf of a Provider. It is an interface so
// that a test can assert which browser would be started, on which profile,
// without a browser ever running.
type Launcher interface {
	// Available reports whether probe exists on this host. An absolute probe is
	// a path to stat, typically a macOS application bundle; anything else is a
	// binary name to look up in PATH.
	Available(probe string) bool
	// Launch starts name with args as a background process and returns as soon
	// as it is started, never waiting for it to exit.
	Launch(name string, args []string) error
}

// ExecLauncher is the Launcher the axf binary uses: it runs real processes.
//
// The zero value is ready to use.
type ExecLauncher struct{}

// Available stats an absolute probe and looks up anything else in PATH.
func (ExecLauncher) Available(probe string) bool {
	if probe == "" {
		return false
	}
	if filepath.IsAbs(probe) {
		_, err := os.Stat(probe)
		return err == nil
	}
	_, err := exec.LookPath(probe)
	return err == nil
}

// Launch starts the process and detaches from it, so `axf up` returns without
// waiting for a browser window to be closed.
//
// The command's standard streams are left nil, which os/exec wires to
// /dev/null: inheriting them would keep the `$(axf up ...)` command
// substitution of the calling shell open for as long as the browser runs.
func (ExecLauncher) Launch(name string, args []string) error {
	// A context that is never cancelled: cancelling would kill the browser,
	// which must outlive this process.
	// name is always a literal from the fixed browserCommand table in
	// browser.go (never document-controlled); args are those literals plus a
	// traversal-checked profile directory.
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // G204: name is a fixed, non-document-controlled literal
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("runtime: starting %s: %w", name, err)
	}
	// Releasing rather than waiting is what leaves the process running once axf
	// exits.
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("runtime: detaching from %s: %w", name, err)
	}
	return nil
}
