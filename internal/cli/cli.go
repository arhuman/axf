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
  axf audit                print the Alter Guard audit trail
  axf keys generate <name> create the decryption identity of an Alter, print its recipient
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

The Alter Guard applies policies[] to the activation path: a matching deny with
scope runtime stops axf up before it prints anything, one with scope guard only
records the decision. Every capability action of up and down is appended to
$AXF_HOME/audit.log, which axf audit prints. Deactivation is audited but never
blocked, so axf down always returns a shell to a clean state.

axf keys generate writes an age identity to $AXF_HOME/keys/<name>.age and prints
its recipient on stdout. Add that age1... line to assets[].encryption.recipients[]
of the Alter's inline Assets by hand; nothing edits a document for you. The
identity is scoped to one Alter and is never overwritten: rotating means removing
the file first, which makes every Asset encrypted for it unreadable.

This build covers the data model, the v1 runtime (the shell, git-identity,
browser-profile and ssh-keypair capabilities, plus lifecycle hook execution) and
the v1.1 Alter Guard. Inline Assets are decrypted with age; signature
verification is not implemented.
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
	case "audit":
		return runAudit(args[1:], stdout, stderr)
	case "keys":
		return runKeys(args[1:], stdout, stderr)
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

// runAudit dumps the Alter Guard trail verbatim, one JSON event per line, so it
// pipes straight into jq. An absent trail is not a failure: it only means no
// Alter has been activated from this $AXF_HOME yet.
func runAudit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "axf audit: expected no argument\n\n%s", usage)
		return ExitUsage
	}
	home, err := runtime.Home()
	if err != nil {
		report(stderr, "axf audit", err)
		return ExitError
	}
	path := runtime.Store{Home: home}.AuditPath()
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is Home+"audit.log", no variable component
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stderr, "axf audit: no audit trail yet (%s)\n", path)
		return ExitOK
	case err != nil:
		report(stderr, "axf audit", fmt.Errorf("cli: reading %s: %w", path, err))
		return ExitError
	}
	if _, err := stdout.Write(data); err != nil {
		report(stderr, "axf audit", fmt.Errorf("cli: writing the audit trail: %w", err))
		return ExitError
	}
	return ExitOK
}

// runKeys creates the local decryption identity of one Alter. Only the
// recipient reaches stdout, so `axf keys generate x | pbcopy` yields exactly the
// line to paste into assets[].encryption.recipients[]; the path goes to stderr.
func runKeys(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "generate" {
		fmt.Fprintf(stderr, "axf keys: expected `axf keys generate <name>`\n\n%s", usage)
		return ExitUsage
	}
	home, err := runtime.Home()
	if err != nil {
		report(stderr, "axf keys generate", err)
		return ExitError
	}
	path, recipient, err := runtime.GenerateIdentity(home, args[1])
	if err != nil {
		report(stderr, "axf keys generate", err)
		return ExitError
	}
	fmt.Fprintln(stdout, recipient)
	fmt.Fprintf(stderr, "axf keys generate: identity written to %s\n", path)
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

// validateFile reads an operator-named local file, `axf validate <file>`'s
// entire purpose, the same trust model as `cat` or `jq`.
func validateFile(path string, stdout io.Writer) error {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is a CLI argument by design, see doc comment
	if err != nil {
		return fmt.Errorf("cli: reading %s: %w", path, err)
	}
	doc, err := conformance.ParseAndCheck(data)
	if err != nil {
		return err
	}
	for _, warning := range conformance.Warnings(doc) {
		fmt.Fprintf(stdout, "%s: warning: %s\n", path, warning)
	}
	fmt.Fprintf(stdout, "%s: OK (%s)\n", path, doc.Metadata.ID)
	return nil
}
