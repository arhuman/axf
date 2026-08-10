package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	alter "github.com/arhuman/axf"
	"github.com/arhuman/axf/conformance"
)

// HomeEnv is the environment variable overriding the AXF runtime root.
const HomeEnv = "AXF_HOME"

// ErrAlterNotFound reports a store name with no document behind it.
var ErrAlterNotFound = errors.New("runtime: no such Alter")

// ErrInvalidAlterName reports a store name unusable as a file name.
var ErrInvalidAlterName = errors.New("runtime: invalid Alter name")

// Home returns the AXF runtime root: $AXF_HOME when set, ~/.axf otherwise.
//
// The override is what lets a test point the runtime at a temporary directory
// and never read or write the real home directory.
func Home() (string, error) {
	if home := os.Getenv(HomeEnv); home != "" {
		return home, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("runtime: locating the home directory: %w", err)
	}
	return filepath.Join(dir, ".axf"), nil
}

// Store reads Alter documents from $AXF_HOME/alters/<name>.json.
//
// That layout is a runtime convention, not part of AXF: the format leaves
// storage to the runtime (spec section 3, Provider-based). <name> is the file
// name stem and is not required to equal the document's metadata.name.
type Store struct {
	// Home is the runtime root the store reads from.
	Home string
}

// AuditPath returns the Alter Guard audit trail of this runtime root:
// $AXF_HOME/audit.log, a JSON Lines file the Guard only ever appends to.
func (s Store) AuditPath() string {
	return filepath.Join(s.Home, "audit.log")
}

// AlterPath returns the file a store name resolves to, without reading it.
func (s Store) AlterPath(name string) (string, error) {
	if err := checkAlterName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.Home, "alters", name+".json"), nil
}

// Load reads the named Alter, validates it against the AXF v0 schema and
// applies the conformance rules. A document failing either is never activated,
// so the runtime only ever acts on a document the SDK considers well formed.
//
// A missing document yields an error wrapping ErrAlterNotFound that names both
// the store name and the path looked at.
func (s Store) Load(name string) (*alter.Alter, error) {
	path, err := s.AlterPath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: no Alter named %q in $AXF_HOME/alters (looked for %s)",
				ErrAlterNotFound, name, path)
		}
		return nil, fmt.Errorf("runtime: reading %s: %w", path, err)
	}
	doc, err := alter.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("runtime: %s: %w", path, err)
	}
	if err := conformance.Check(doc); err != nil {
		return nil, fmt.Errorf("runtime: %s: %w", path, err)
	}
	return doc, nil
}

// checkAlterName rejects anything that is not a bare file name stem. The name
// reaches the filesystem, so a separator or a parent-directory element would
// turn `axf up` into an arbitrary file read.
func checkAlterName(name string) error {
	return checkFileNameStem(ErrInvalidAlterName, name)
}

// checkFileNameStem rejects a name that is not a bare file name stem, reporting
// it under kind. Every document-supplied name axf turns into a path goes through
// it: an Alter store name (see checkAlterName) and an assets[].name reaching
// $AXF_HOME/keys/ssh (see writeInlineKey) share the traversal exposure.
func checkFileNameStem(kind error, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name is empty", kind)
	case name == "." || name == "..":
		return fmt.Errorf("%w: %q", kind, name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("%w: %q contains a path separator", kind, name)
	case strings.ContainsRune(name, 0):
		return fmt.Errorf("%w: %q contains a NUL byte", kind, name)
	}
	return nil
}
