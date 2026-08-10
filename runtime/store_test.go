package runtime_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arhuman/axf/runtime"
)

// AXF_HOME is what keeps a test away from the real home directory, so its
// precedence over ~/.axf is part of the contract.
func TestHome(t *testing.T) {
	t.Run("honours AXF_HOME", func(t *testing.T) {
		want := t.TempDir()
		t.Setenv(runtime.HomeEnv, want)
		got, err := runtime.Home()
		if err != nil {
			t.Fatalf("Home() error = %v", err)
		}
		if got != want {
			t.Errorf("Home() = %q, want %q", got, want)
		}
	})

	t.Run("defaults to ~/.axf", func(t *testing.T) {
		t.Setenv(runtime.HomeEnv, "")
		got, err := runtime.Home()
		if err != nil {
			t.Fatalf("Home() error = %v", err)
		}
		if filepath.Base(got) != ".axf" {
			t.Errorf("Home() = %q, want it to end in .axf", got)
		}
	})
}

// A store name reaches the filesystem, so it must stay a bare file name stem.
func TestStoreAlterPathRejectsTraversal(t *testing.T) {
	store := runtime.Store{Home: "/tmp/axf-home"}
	for _, name := range []string{"", ".", "..", "../escape", "sub/alter", `back\slash`, "nul\x00byte"} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.AlterPath(name); !errors.Is(err, runtime.ErrInvalidAlterName) {
				t.Errorf("AlterPath(%q) error = %v, want it to wrap ErrInvalidAlterName", name, err)
			}
		})
	}
}

func TestStoreAlterPath(t *testing.T) {
	store := runtime.Store{Home: "/tmp/axf-home"}
	got, err := store.AlterPath("alchemist")
	if err != nil {
		t.Fatalf("AlterPath() error = %v", err)
	}
	want := filepath.Join("/tmp/axf-home", "alters", "alchemist.json")
	if got != want {
		t.Errorf("AlterPath() = %q, want %q", got, want)
	}
}

func TestStoreLoad(t *testing.T) {
	home := newHome(t, "alchemist")
	store := runtime.Store{Home: home}

	t.Run("resolves by file name, not by metadata.name", func(t *testing.T) {
		doc, err := store.Load("alchemist")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if doc.Metadata.Name != "systems-alchemist" {
			t.Errorf("metadata.name = %q, want the document's own label", doc.Metadata.Name)
		}
	})

	t.Run("reports a missing document with the path", func(t *testing.T) {
		_, err := store.Load("ghost")
		if !errors.Is(err, runtime.ErrAlterNotFound) {
			t.Fatalf("Load() error = %v, want it to wrap ErrAlterNotFound", err)
		}
		if !strings.Contains(err.Error(), filepath.Join(home, "alters", "ghost.json")) {
			t.Errorf("Load() error = %v, want it to name the path looked at", err)
		}
	})

	t.Run("rejects a document failing schema validation", func(t *testing.T) {
		path := filepath.Join(home, "alters", "broken.json")
		if err := os.WriteFile(path, []byte(`{"apiVersion":"axf/v1","kind":"Alter"}`), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		if _, err := store.Load("broken"); err == nil {
			t.Error("Load() error = nil, want the schema violation")
		}
	})

	t.Run("rejects a document exceeding the size cap", func(t *testing.T) {
		path := filepath.Join(home, "alters", "huge.json")
		// One byte over the 16 MiB cap; the content need not be valid JSON,
		// since the cap is checked before parsing.
		if err := os.WriteFile(path, []byte(strings.Repeat("a", 16<<20+1)), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		_, err := store.Load("huge")
		if err == nil {
			t.Fatal("Load() error = nil, want the size cap to reject it")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("Load() error = %v, want it to mention the size cap", err)
		}
	})
}
