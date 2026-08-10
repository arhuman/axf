package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/axf/internal/cli"
)

// alterFixtures holds the Alter documents these tests activate. None of them
// declares a browser-profile capability: the CLI wires a real process launcher,
// so a fixture asking for a browser would start one.
const alterFixtures = "../../testdata/alters"

// setupHome points AXF_HOME at a temporary directory holding the named fixture
// Alters and reports that no Alter is active, so no test touches the real ~/.axf
// or inherits the developer's own active Alter.
func setupHome(t *testing.T, names ...string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "alters")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(alterFixtures, name+".json"))
		if err != nil {
			t.Fatalf("reading fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Setenv("AXF_HOME", home)
	t.Setenv("AXF_ACTIVE_ALTER", "")
}

// Another shell evaluates this output verbatim, so its exact text is the
// contract: order, POSIX quoting of values holding a space or a quote, and the
// active marker last.
func TestUpPrintsAnEvaluableScript(t *testing.T) {
	setupHome(t, "alchemist")
	code, stdout, stderr := run(t, "up", "alchemist")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", code, cli.ExitOK, stderr)
	}
	want := "export GIT_AUTHOR_EMAIL='alchemist@example.com'\n" +
		"export GIT_AUTHOR_NAME='Alchemist O'\\''Brien'\n" +
		"export GIT_COMMITTER_EMAIL='alchemist@example.com'\n" +
		"export GIT_COMMITTER_NAME='Alchemist O'\\''Brien'\n" +
		"export AXF_ALTER_NAME='alchemist'\n" +
		"export AXF_PROMPT_LABEL='alchemist prime'\n" +
		"export AXF_ACTIVE_ALTER='alchemist'\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// Switching Alters must undo the previous one first, which the runtime learns
// only from AXF_ACTIVE_ALTER in its own environment.
func TestUpSwitchesFromTheActiveAlter(t *testing.T) {
	setupHome(t, "alchemist", "researcher")
	t.Setenv("AXF_ACTIVE_ALTER", "alchemist")

	code, stdout, stderr := run(t, "up", "researcher")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", code, cli.ExitOK, stderr)
	}
	want := "unset GIT_AUTHOR_EMAIL\n" +
		"unset GIT_AUTHOR_NAME\n" +
		"unset GIT_COMMITTER_EMAIL\n" +
		"unset GIT_COMMITTER_NAME\n" +
		"unset AXF_ALTER_NAME\n" +
		"unset AXF_PROMPT_LABEL\n" +
		"unset AXF_ACTIVE_ALTER\n" +
		"export AXF_ALTER_NAME='researcher'\n" +
		"export AXF_PROMPT_LABEL='researcher'\n" +
		"export AXF_ACTIVE_ALTER='researcher'\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestUpReportsAMissingAlter(t *testing.T) {
	setupHome(t)
	code, stdout, stderr := run(t, "up", "ghost")
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing to evaluate", stdout)
	}
	for _, want := range []string{`no Alter named "ghost"`, "looked for"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// A capability with no provider stops the activation before anything is
// printed, and says why rather than reporting an unknown name.
func TestUpFailsClosedOnAnUnimplementedCapability(t *testing.T) {
	setupHome(t, "ssh-only")
	code, stdout, stderr := run(t, "up", "ssh-only")
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing to evaluate", stdout)
	}
	for _, want := range []string{"provider not implemented in v1: ssh-keypair", "requires asset decryption"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// A hook the runtime cannot honour is a warning on stderr and a non-zero exit,
// but stdout stays a correct script: one unimplemented hook must not cost the
// user the rest of their identity.
func TestUpDegradesOnAnUnimplementedHook(t *testing.T) {
	setupHome(t, "hooked")
	code, stdout, stderr := run(t, "up", "hooked")
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	want := "export AXF_ALTER_NAME='hooked'\n" +
		"export AXF_PROMPT_LABEL='hooked'\n" +
		"export AXF_ACTIVE_ALTER='hooked'\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if !strings.Contains(stderr, "hook {capability: ssh-keypair, action: load}") {
		t.Errorf("stderr = %q, want it to name the hook that did not run", stderr)
	}
	if strings.Contains(stderr, "shell") {
		t.Errorf("stderr = %q, want the shell start hook to be a silent no-op", stderr)
	}
}

func TestUpWithoutAName(t *testing.T) {
	setupHome(t)
	code, _, stderr := run(t, "up")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(stderr, "expected exactly one Alter name") {
		t.Errorf("stderr = %q, want the usage error", stderr)
	}
}

func TestDown(t *testing.T) {
	setupHome(t, "alchemist")
	t.Setenv("AXF_ACTIVE_ALTER", "alchemist")

	code, stdout, stderr := run(t, "down")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", code, cli.ExitOK, stderr)
	}
	want := "unset GIT_AUTHOR_EMAIL\n" +
		"unset GIT_AUTHOR_NAME\n" +
		"unset GIT_COMMITTER_EMAIL\n" +
		"unset GIT_COMMITTER_NAME\n" +
		"unset AXF_ALTER_NAME\n" +
		"unset AXF_PROMPT_LABEL\n" +
		"unset AXF_ACTIVE_ALTER\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// Deactivating with nothing active is a harmless no-op: a shell rc or a script
// may call it unconditionally.
func TestDownWithNothingActive(t *testing.T) {
	setupHome(t)
	code, stdout, stderr := run(t, "down")
	if code != cli.ExitOK {
		t.Errorf("exit code = %d, want %d", code, cli.ExitOK)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed", stdout)
	}
	if !strings.Contains(stderr, "no Alter is active") {
		t.Errorf("stderr = %q, want the friendly message", stderr)
	}
}

// A shell must always be able to get back to a clean state, even after the
// document behind the active Alter was deleted.
func TestDownClearsTheMarkerWhenTheDocumentIsGone(t *testing.T) {
	setupHome(t)
	t.Setenv("AXF_ACTIVE_ALTER", "vanished")

	code, stdout, stderr := run(t, "down")
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	if stdout != "unset AXF_ACTIVE_ALTER\n" {
		t.Errorf("stdout = %q, want the active marker cleared anyway", stdout)
	}
	if !strings.Contains(stderr, "vanished") {
		t.Errorf("stderr = %q, want the missing document reported", stderr)
	}
}

func TestDownRejectsArguments(t *testing.T) {
	setupHome(t)
	code, _, stderr := run(t, "down", "alchemist")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(stderr, "expected no argument") {
		t.Errorf("stderr = %q, want the usage error", stderr)
	}
}

// A policy denial stops the activation before stdout gets a single line, the
// same fail-closed posture an unimplemented capability has.
func TestUpFailsClosedOnADeniedCapability(t *testing.T) {
	setupHome(t, "guarded")
	code, stdout, stderr := run(t, "up", "guarded")
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing to evaluate", stdout)
	}
	for _, want := range []string{"denied by an Alter Guard policy", "capability: shell", "policies[0]"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// axf audit prints the trail verbatim, one JSON event per line, so it pipes
// into jq unchanged.
func TestAuditPrintsTheTrail(t *testing.T) {
	setupHome(t, "alchemist")
	if code, _, stderr := run(t, "up", "alchemist"); code != cli.ExitOK {
		t.Fatalf("axf up exit code = %d (stderr=%q)", code, stderr)
	}
	code, stdout, stderr := run(t, "audit")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", code, cli.ExitOK, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("audit lines = %d, want one per capability:\n%s", len(lines), stdout)
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("audit line %q is not JSON: %v", line, err)
		}
		for _, field := range []string{"timestamp", "alterId", "actor", "capability", "action", "provider", "result"} {
			if _, ok := event[field]; !ok {
				t.Errorf("audit line %q has no %q field", line, field)
			}
		}
		if event["result"] != "allowed" {
			t.Errorf("audit line %q result = %v, want allowed", line, event["result"])
		}
	}
}

// An empty trail is not a failure: it only means nothing has been activated
// from this $AXF_HOME yet.
func TestAuditWithNothingRecordedYet(t *testing.T) {
	setupHome(t)
	code, stdout, stderr := run(t, "audit")
	if code != cli.ExitOK {
		t.Errorf("exit code = %d, want %d", code, cli.ExitOK)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed", stdout)
	}
	if !strings.Contains(stderr, "no audit trail yet") {
		t.Errorf("stderr = %q, want the friendly message", stderr)
	}
}

func TestAuditRejectsArguments(t *testing.T) {
	setupHome(t)
	code, _, stderr := run(t, "audit", "alchemist")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(stderr, "expected no argument") {
		t.Errorf("stderr = %q, want the usage error", stderr)
	}
}

func TestHelpDocumentsActivation(t *testing.T) {
	code, stdout, _ := run(t, "help")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitOK)
	}
	for _, want := range []string{"axf up <name>", "axf down", "axf audit", `eval "$(axf up`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help = %q, want it to contain %q", stdout, want)
		}
	}
}
