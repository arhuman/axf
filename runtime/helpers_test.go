package runtime_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/arhuman/axf/runtime"
)

// alterFixtures holds the Alter documents the runtime tests activate.
const alterFixtures = "../testdata/alters"

// newHome builds an AXF_HOME in a temporary directory holding the named fixture
// Alters. Every test goes through it so none ever reads or writes the real
// ~/.axf, and so profile directories are created under the temporary root.
func newHome(t *testing.T, names ...string) string {
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
	return home
}

// auditTrail reads back the audit log of a home as decoded events. A home with
// no trail yet yields none, which is how a test asserts nothing was audited.
func auditTrail(t *testing.T, home string) []runtime.AuditEvent {
	t.Helper()
	data, err := os.ReadFile(runtime.Store{Home: home}.AuditPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the audit trail: %v", err)
	}
	var events []runtime.AuditEvent
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var e runtime.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("audit line %q is not one JSON event: %v", scanner.Text(), err)
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning the audit trail: %v", err)
	}
	return events
}

// launched records one process a fakeLauncher was asked to start.
type launched struct {
	Name string
	Args []string
}

// fakeLauncher reports as installed only the probes it was given and records
// what it was asked to start, so a test can assert on browser selection without
// a browser ever running.
type fakeLauncher struct {
	installed []string
	launches  []launched
	err       error
}

func (f *fakeLauncher) Available(probe string) bool {
	return slices.Contains(f.installed, probe)
}

func (f *fakeLauncher) Launch(name string, args []string) error {
	f.launches = append(f.launches, launched{Name: name, Args: slices.Clone(args)})
	return f.err
}
