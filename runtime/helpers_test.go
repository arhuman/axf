package runtime_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
