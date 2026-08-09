package conformance_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	alter "github.com/arhuman/axf"
	"github.com/arhuman/axf/conformance"
)

const fixtureDir = "../testdata/fixtures"

func parseFixture(t *testing.T, rel string) *alter.Alter {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, rel))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	doc, err := alter.Parse(data)
	if err != nil {
		t.Fatalf("fixture %s must pass schema validation before conformance: %v", rel, err)
	}
	return doc
}

// TestCheckFixtures is the spec section 17 format suite: every fixture an AXF
// implementation must classify identically.
func TestCheckFixtures(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		wantErr    error
		wantWarned bool
	}{
		{
			name:    "canonical instance is conformant",
			fixture: "valid/canonical-instance.json",
		},
		{
			name:    "minimal document is conformant",
			fixture: "valid/minimal.json",
		},
		{
			name:    "extension fields at every level stay conformant",
			fixture: "valid/extension-fields-at-every-level.json",
		},
		{
			name:       "inline asset without a recovery key warns but stays valid",
			fixture:    "valid/inline-asset-without-recovery-key.json",
			wantWarned: true,
		},
		{
			name:    "two assets sharing a name are rejected",
			fixture: "invalid/duplicate-asset-name.json",
			wantErr: conformance.ErrDuplicateAssetName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseFixture(t, tt.fixture)
			err := conformance.Check(doc)
			switch {
			case tt.wantErr == nil && err != nil:
				t.Fatalf("Check = %v, want nil", err)
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Fatalf("Check = %v, want an error matching %v", err, tt.wantErr)
			}
			if warned := len(conformance.Warnings(doc)) > 0; warned != tt.wantWarned {
				t.Errorf("warnings present = %v, want %v (%v)", warned, tt.wantWarned, conformance.Warnings(doc))
			}
		})
	}
}

// The schema already requires ownerSignature on every asset, so this fixture
// must never reach Check: it is rejected one layer earlier.
func TestInlineAssetMissingSignatureIsRejectedByTheSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixtureDir, "invalid/inline-asset-missing-signature.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	if err := alter.Validate(data); err == nil {
		t.Fatal("expected the schema to reject an inline asset without ownerSignature")
	}
}

func TestCheckReportsEveryDuplicate(t *testing.T) {
	doc := &alter.Alter{
		Assets: []alter.Asset{
			{Name: "a"}, {Name: "b"}, {Name: "a"}, {Name: "b"}, {Name: "c"},
		},
	}
	err := conformance.Check(doc)
	if err == nil {
		t.Fatal("expected duplicate errors, got nil")
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("expected a joined error, got %T", err)
	}
	if got := len(joined.Unwrap()); got != 2 {
		t.Errorf("reported %d duplicates, want 2: %v", got, err)
	}
}

func TestNilAlter(t *testing.T) {
	if err := conformance.Check(nil); err == nil {
		t.Error("Check(nil) = nil, want an error")
	}
	if got := conformance.Warnings(nil); got != nil {
		t.Errorf("Warnings(nil) = %v, want nil", got)
	}
}

func TestWarningsOnlyCoverInlineAssets(t *testing.T) {
	doc := &alter.Alter{
		Metadata: alter.Metadata{Owner: &alter.Owner{KeyType: alter.KeyTypeEd25519}},
		Assets: []alter.Asset{
			{Name: "external", Kind: alter.AssetKindRef},
			{Name: "embedded", Kind: alter.AssetKindInline},
		},
	}
	warnings := conformance.Warnings(doc)
	if len(warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly one for the inline asset", warnings)
	}

	doc.Metadata.Owner.RecoveryKeyFingerprint = "sha256:bb"
	if got := conformance.Warnings(doc); got != nil {
		t.Errorf("Warnings = %v, want nil once a recovery key is configured", got)
	}
}
