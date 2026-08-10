package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/axf/internal/cli"
)

// Only the recipient reaches stdout, so the command pipes straight into a
// clipboard or an editor; the path it wrote goes to stderr.
func TestKeysGenerate(t *testing.T) {
	setupHome(t)
	code, stdout, stderr := run(t, "keys", "generate", "alchemist")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", code, cli.ExitOK, stderr)
	}
	recipient := strings.TrimSpace(stdout)
	if !strings.HasPrefix(recipient, "age1") {
		t.Errorf("stdout = %q, want an age1... recipient", stdout)
	}
	path := filepath.Join(os.Getenv("AXF_HOME"), "keys", "alchemist.age")
	if !strings.Contains(stderr, path) {
		t.Errorf("stderr = %q, want it to name %s", stderr, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the identity: %v", err)
	}
	if !strings.Contains(string(data), recipient) {
		t.Errorf("identity file = %q, want it to carry the printed recipient", data)
	}
}

// Generating twice must not destroy the identity every existing ciphertext was
// encrypted for.
func TestKeysGenerateRefusesToOverwrite(t *testing.T) {
	setupHome(t)
	if code, _, stderr := run(t, "keys", "generate", "alchemist"); code != cli.ExitOK {
		t.Fatalf("first run exit code = %d, want %d (stderr=%q)", code, cli.ExitOK, stderr)
	}
	code, stdout, stderr := run(t, "keys", "generate", "alchemist")
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing printed", stdout)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("stderr = %q, want it to report the existing identity", stderr)
	}
}

// The name becomes a file name, so it is refused on the same grounds a store
// name is.
func TestKeysGenerateRejectsAnInvalidName(t *testing.T) {
	for _, name := range []string{".", "..", "../escape", "sub/alter", `back\slash`} {
		t.Run(name, func(t *testing.T) {
			setupHome(t)
			code, stdout, stderr := run(t, "keys", "generate", name)
			if code != cli.ExitError {
				t.Errorf("exit code = %d, want %d", code, cli.ExitError)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing printed", stdout)
			}
			if !strings.Contains(stderr, "invalid Alter name") {
				t.Errorf("stderr = %q, want the invalid name reported", stderr)
			}
		})
	}
}

func TestKeysUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no subcommand", args: []string{"keys"}},
		{name: "unknown subcommand", args: []string{"keys", "rotate", "alchemist"}},
		{name: "generate without a name", args: []string{"keys", "generate"}},
		{name: "generate with two names", args: []string{"keys", "generate", "a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupHome(t)
			code, _, stderr := run(t, tt.args...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if !strings.Contains(stderr, "axf keys generate <name>") {
				t.Errorf("stderr = %q, want the usage line", stderr)
			}
		})
	}
}
