package alter_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	alter "github.com/arhuman/axf"
)

const (
	validDir   = "testdata/fixtures/valid"
	invalidDir = "testdata/fixtures/invalid"
)

// schemaValidExceptions lists fixtures that live under invalid/ because an
// Alter must reject them, yet pass JSON Schema validation: their rule is a
// conformance rule the schema cannot express (spec section 17).
var schemaValidExceptions = map[string]string{
	"duplicate-asset-name.json": "asset name uniqueness is a conformance rule, not a schema constraint",
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func fixturePaths(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("globbing %s: %v", dir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no fixtures found in %s", dir)
	}
	return paths
}

func TestValidateAcceptsValidFixtures(t *testing.T) {
	for _, path := range fixturePaths(t, validDir) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := alter.Validate(readFixture(t, path)); err != nil {
				t.Fatalf("expected fixture to validate, got: %v", err)
			}
		})
	}
}

func TestValidateRejectsInvalidFixtures(t *testing.T) {
	for _, path := range fixturePaths(t, invalidDir) {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			err := alter.Validate(readFixture(t, path))
			if reason, exempt := schemaValidExceptions[name]; exempt {
				if err != nil {
					t.Fatalf("expected schema to accept this fixture (%s), got: %v", reason, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected schema validation to fail, got nil")
			}
		})
	}
}

func TestParseRoundTripsValidFixtures(t *testing.T) {
	for _, path := range fixturePaths(t, validDir) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			original := readFixture(t, path)
			doc, err := alter.Parse(original)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			reencoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if err := alter.Validate(reencoded); err != nil {
				t.Fatalf("re-encoded document no longer validates: %v", err)
			}
			if got, want := decodeGeneric(t, reencoded), decodeGeneric(t, original); !reflect.DeepEqual(got, want) {
				t.Errorf("round trip lost or altered data\n got: %s\nwant: %s", reencoded, original)
			}
		})
	}
}

func decodeGeneric(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decoding for comparison: %v", err)
	}
	return v
}

func TestParseCanonicalInstanceFields(t *testing.T) {
	doc, err := alter.Parse(readFixture(t, filepath.Join(validDir, "canonical-instance.json")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if doc.APIVersion != alter.APIVersion || doc.Kind != alter.Kind {
		t.Errorf("apiVersion/kind = %q/%q, want %q/%q", doc.APIVersion, doc.Kind, alter.APIVersion, alter.Kind)
	}
	if doc.Metadata.ID != "urn:axf:alter:6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e" {
		t.Errorf("metadata.id = %q", doc.Metadata.ID)
	}
	if doc.Metadata.Owner == nil {
		t.Fatal("metadata.owner is nil")
	}
	if doc.Metadata.Owner.KeyType != alter.KeyTypeEd25519 {
		t.Errorf("owner.keyType = %q", doc.Metadata.Owner.KeyType)
	}
	if got := doc.Metadata.Owner.EffectiveStatus(); got != alter.OwnerActive {
		t.Errorf("owner status = %q, want %q", got, alter.OwnerActive)
	}
	if doc.Metadata.Owner.KeyEventLogURI == "" {
		t.Error("owner.keyEventLogUri lost during decode")
	}

	if len(doc.Assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(doc.Assets))
	}
	ref, inline := doc.Assets[0], doc.Assets[1]
	if ref.Kind != alter.AssetKindRef || ref.URI == "" {
		t.Errorf("assets[0] = %+v, want a ref asset with a uri", ref)
	}
	if inline.Kind != alter.AssetKindInline || inline.Encryption == nil {
		t.Fatalf("assets[1] = %+v, want an inline asset with encryption", inline)
	}
	if got := len(inline.Encryption.Recipients); got != 2 {
		t.Errorf("len(assets[1].encryption.recipients) = %d, want 2", got)
	}
	for i, asset := range doc.Assets {
		if asset.OwnerSignature == nil {
			t.Errorf("assets[%d].ownerSignature is nil", i)
			continue
		}
		if asset.OwnerSignature.Canonicalization != alter.CanonicalizationJCS {
			t.Errorf("assets[%d].ownerSignature.canonicalization = %q", i, asset.OwnerSignature.Canonicalization)
		}
	}

	if len(doc.Policies) != 1 {
		t.Fatalf("len(policies) = %d, want 1", len(doc.Policies))
	}
	policy := doc.Policies[0]
	if policy.Effect != alter.EffectDeny {
		t.Errorf("policies[0].effect = %q", policy.Effect)
	}
	if policy.EffectiveScope() != alter.ScopeRuntime {
		t.Errorf("policies[0].scope = %q", policy.EffectiveScope())
	}
	if policy.Condition == nil || policy.Condition.Type != alter.ConditionActiveAlterName {
		t.Errorf("policies[0].condition = %+v", policy.Condition)
	}
	if !policy.RedactSecrets() {
		t.Error("policies[0].audit.redactSecrets = false, want true")
	}

	if doc.Lifecycle == nil || doc.Lifecycle.Hooks == nil {
		t.Fatal("lifecycle.hooks is nil")
	}
	if got := doc.Lifecycle.Hooks.PostActivation; len(got) != 1 || got[0].Capability != "shell" {
		t.Errorf("lifecycle.hooks.postActivation = %+v", got)
	}
	if doc.Signature == nil || doc.Signature.Algorithm != alter.KeyTypeEd25519 {
		t.Errorf("signature = %+v", doc.Signature)
	}
	if doc.Context == nil || doc.Context.Locale != "fr-FR" {
		t.Errorf("context = %+v", doc.Context)
	}
	if _, ok := doc.Context.Custom["x-doolta"]; !ok {
		t.Errorf("context.custom lost the x-doolta entry: %v", doc.Context.Custom)
	}
	if _, ok := doc.Capabilities[0].Config["email"]; !ok {
		t.Errorf("capabilities[0].config lost the email entry: %v", doc.Capabilities[0].Config)
	}
}

func TestValidateRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		data string
		want error
	}{
		{name: "empty input", data: ""},
		{name: "truncated object", data: `{"apiVersion":`},
		{name: "not an object", data: `"axf/v0"`},
		{name: "trailing document", data: `{"apiVersion":"axf/v0","kind":"Alter","metadata":{"id":"urn:axf:alter:00000000-0000-4000-8000-000000000000","name":"a"}} {}`, want: alter.ErrTrailingData},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := alter.Validate([]byte(tt.data))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParsePropagatesValidationError(t *testing.T) {
	doc, err := alter.Parse(readFixture(t, filepath.Join(invalidDir, "inline-asset-missing-signature.json")))
	if err == nil {
		t.Fatal("expected Parse to reject an inline asset without ownerSignature")
	}
	if doc != nil {
		t.Errorf("expected a nil Alter on error, got %+v", doc)
	}
}

func TestSchemaReturnsAnIndependentCopy(t *testing.T) {
	first := alter.Schema()
	if len(first) == 0 {
		t.Fatal("Schema returned no bytes")
	}
	var doc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatalf("embedded schema is not valid JSON: %v", err)
	}
	if doc.ID != alter.SchemaID {
		t.Errorf("$id = %q, want %q", doc.ID, alter.SchemaID)
	}

	first[0] = 'x'
	if second := alter.Schema(); second[0] == 'x' {
		t.Error("Schema exposes the embedded bytes: mutating one copy corrupted the next")
	}
}
