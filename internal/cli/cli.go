// Package cli implements the axf command surface. It is internal because the
// importable API is the alter and conformance packages, not this wiring.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	alter "github.com/arhuman/axf"
	"github.com/arhuman/axf/conformance"
	"github.com/arhuman/axf/internal/version"
	"github.com/arhuman/axf/runtime"
)

// Exit codes returned by Run.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

const usage = `axf reads, validates and activates AXF (Alter eXtensible Format) v0 documents.

Usage:
  axf up <name>            print the shell commands activating an Alter
  axf down                 print the shell commands deactivating the active Alter
  axf validate <file>...   validate documents against the AXF v0 schema and conformance rules
  axf schema               print the embedded JSON Schema
  axf version              print build information
  axf help                 print this message

up and down write shell commands to stdout for the calling shell to evaluate:

  eval "$(axf up systems-alchemist)"
  eval "$(axf down)"

Alters are read from $AXF_HOME/alters/<name>.json, $AXF_HOME defaulting to
~/.axf. Which Alter is active is carried by AXF_ACTIVE_ALTER in the shell that
evaluated axf up; nothing is stored on disk.

This build covers the data model and the v1 runtime: the shell, git-identity
and browser-profile capabilities, plus lifecycle hook execution. Policy
enforcement and the audit trail belong to the Alter Guard and are not
implemented yet.
`

// Run executes one axf invocation and returns the process exit code. args are
// the arguments after the program name. Run never calls os.Exit, so it is
// directly testable.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "up":
		return runUp(args[1:], stdout, stderr)
	case "down":
		return runDown(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "schema":
		fmt.Fprintf(stdout, "%s\n", alter.Schema())
		return ExitOK
	case "version":
		fmt.Fprintln(stdout, version.String())
		return ExitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "axf: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}

// runUp activates one Alter. Its stdout is meant to be eval'd, so every
// diagnostic goes to stderr, including the warnings of a partial activation
// whose printed commands are still correct.
func runUp(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(stderr, "axf up: expected exactly one Alter name\n\n%s", usage)
		return ExitUsage
	}
	rt, err := newRuntime()
	if err != nil {
		report(stderr, "axf up", err)
		return ExitError
	}
	if err := rt.Up(args[0], stdout); err != nil {
		report(stderr, "axf up", err)
		return ExitError
	}
	return ExitOK
}

// runDown deactivates the active Alter. Deactivating with nothing active is a
// no-op, not a failure: a shell rc or a script may call it unconditionally.
func runDown(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "axf down: expected no argument\n\n%s", usage)
		return ExitUsage
	}
	rt, err := newRuntime()
	if err != nil {
		report(stderr, "axf down", err)
		return ExitError
	}
	err = rt.Down(stdout)
	switch {
	case errors.Is(err, runtime.ErrNoActiveAlter):
		fmt.Fprintln(stderr, "axf down: no Alter is active")
		return ExitOK
	case err != nil:
		report(stderr, "axf down", err)
		return ExitError
	}
	return ExitOK
}

// newRuntime builds the runtime from the caller's environment: $AXF_HOME for
// the store, AXF_ACTIVE_ALTER for what the calling shell has in place.
func newRuntime() (*runtime.Runtime, error) {
	home, err := runtime.Home()
	if err != nil {
		return nil, err
	}
	return runtime.New(home, os.Getenv(runtime.AXFActiveAlter), runtime.DefaultRegistry(runtime.ExecLauncher{})), nil
}

// report prints one prefixed line per error. A joined error carries one problem
// per line, and a bare Fprintf would prefix only the first of them.
func report(stderr io.Writer, prefix string, err error) {
	for _, line := range strings.Split(strings.TrimRight(err.Error(), "\n"), "\n") {
		fmt.Fprintf(stderr, "%s: %s\n", prefix, line)
	}
}

func runValidate(paths []string, stdout, stderr io.Writer) int {
	if len(paths) == 0 {
		fmt.Fprintf(stderr, "axf validate: expected at least one file\n\n%s", usage)
		return ExitUsage
	}
	code := ExitOK
	for _, path := range paths {
		if err := validateFile(path, stdout); err != nil {
			fmt.Fprintf(stderr, "%s: FAIL\n%v\n", path, err)
			code = ExitError
		}
	}
	return code
}

func validateFile(path string, stdout io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cli: reading %s: %w", path, err)
	}
	doc, err := alter.Parse(data)
	if err != nil {
		return err
	}
	if err := conformance.Check(doc); err != nil {
		return err
	}
	for _, warning := range conformance.Warnings(doc) {
		fmt.Fprintf(stdout, "%s: warning: %s\n", path, warning)
	}
	fmt.Fprintf(stdout, "%s: OK (%s)\n", path, doc.Metadata.ID)
	return nil
}
