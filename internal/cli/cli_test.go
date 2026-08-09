package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/axf/internal/cli"
)

const fixtureDir = "../../testdata/fixtures"

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "no arguments prints usage",
			args:       nil,
			wantCode:   cli.ExitUsage,
			wantStderr: "Usage:",
		},
		{
			name:       "unknown command",
			args:       []string{"transmute"},
			wantCode:   cli.ExitUsage,
			wantStderr: `unknown command "transmute"`,
		},
		{
			name:       "help",
			args:       []string{"help"},
			wantCode:   cli.ExitOK,
			wantStdout: "axf validate",
		},
		{
			name:       "version",
			args:       []string{"version"},
			wantCode:   cli.ExitOK,
			wantStdout: "axf ",
		},
		{
			name:       "schema prints the embedded schema",
			args:       []string{"schema"},
			wantCode:   cli.ExitOK,
			wantStdout: "https://axf.doolta.com/schema/v0/alter.schema.json",
		},
		{
			name:       "validate without a file",
			args:       []string{"validate"},
			wantCode:   cli.ExitUsage,
			wantStderr: "expected at least one file",
		},
		{
			name:       "validate a conformant document",
			args:       []string{"validate", filepath.Join(fixtureDir, "valid/canonical-instance.json")},
			wantCode:   cli.ExitOK,
			wantStdout: "OK (urn:axf:alter:6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e)",
		},
		{
			name:       "validate warns on an inline asset without a recovery key",
			args:       []string{"validate", filepath.Join(fixtureDir, "valid/inline-asset-without-recovery-key.json")},
			wantCode:   cli.ExitOK,
			wantStdout: "warning: inline asset \"ssh-key\" has no recovery key",
		},
		{
			name:       "validate rejects a schema violation",
			args:       []string{"validate", filepath.Join(fixtureDir, "invalid/wrong-api-version.json")},
			wantCode:   cli.ExitError,
			wantStderr: "schema validation failed",
		},
		{
			name:       "validate rejects a conformance violation the schema accepts",
			args:       []string{"validate", filepath.Join(fixtureDir, "invalid/duplicate-asset-name.json")},
			wantCode:   cli.ExitError,
			wantStderr: "duplicate asset name",
		},
		{
			name:       "validate reports a missing file",
			args:       []string{"validate", filepath.Join(fixtureDir, "valid/does-not-exist.json")},
			wantCode:   cli.ExitError,
			wantStderr: "reading",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stdout=%q stderr=%q)", code, tt.wantCode, stdout, stderr)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout, tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantStderr)
			}
		})
	}
}

// A batch validation must report every file, not stop at the first failure.
func TestValidateContinuesAfterAFailure(t *testing.T) {
	code, stdout, stderr := run(t, "validate",
		filepath.Join(fixtureDir, "invalid/wrong-api-version.json"),
		filepath.Join(fixtureDir, "valid/minimal.json"),
	)
	if code != cli.ExitError {
		t.Errorf("exit code = %d, want %d", code, cli.ExitError)
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr = %q, want a FAIL line", stderr)
	}
	if !strings.Contains(stdout, "minimal.json: OK") {
		t.Errorf("stdout = %q, want the second file to still be validated", stdout)
	}
}
