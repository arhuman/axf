package alter_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	alter "github.com/arhuman/axf"
)

// TestExtensionsCollectedAtEveryLevel is the spec section 13 fixture: an x-*
// member must be accepted, preserved and re-emitted at every object level of
// the schema, never rejected.
func TestExtensionsCollectedAtEveryLevel(t *testing.T) {
	doc, err := alter.Parse(readFixture(t, filepath.Join(validDir, "extension-fields-at-every-level.json")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	levels := map[string]alter.Extensions{
		"root":              doc.Extensions,
		"metadata":          doc.Metadata.Extensions,
		"metadata.owner":    doc.Metadata.Owner.Extensions,
		"context":           doc.Context.Extensions,
		"capabilities[0]":   doc.Capabilities[0].Extensions,
		"assets[0]":         doc.Assets[0].Extensions,
		"assets[0].encr":    doc.Assets[0].Encryption.Extensions,
		"assets[0].ownerSg": doc.Assets[0].OwnerSignature.Extensions,
		"policies[0]":       doc.Policies[0].Extensions,
		"policies[0].cond":  doc.Policies[0].Condition.Extensions,
		"policies[0].audit": doc.Policies[0].Audit.Extensions,
		"lifecycle":         doc.Lifecycle.Extensions,
		"lifecycle.hooks":   doc.Lifecycle.Hooks.Extensions,
		"hooks.pre[0]":      doc.Lifecycle.Hooks.PreActivation[0].Extensions,
		"signature":         doc.Signature.Extensions,
	}
	for level, ext := range levels {
		if len(ext) != 1 {
			t.Errorf("%s: collected %d extensions, want 1: %v", level, len(ext), ext)
		}
	}
}

func TestExtensionsSurviveAnEmptyDocument(t *testing.T) {
	doc, err := alter.Parse(readFixture(t, filepath.Join(validDir, "minimal.json")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Extensions != nil {
		t.Errorf("Extensions = %v, want nil when the document carries none", doc.Extensions)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &members); err != nil {
		t.Fatalf("decoding re-encoded document: %v", err)
	}
	for name := range members {
		switch name {
		case "apiVersion", "kind", "metadata":
		default:
			t.Errorf("unexpected member %q in re-encoded minimal document", name)
		}
	}
}

func TestMarshalAddsExtensionsSetProgrammatically(t *testing.T) {
	doc := &alter.Alter{
		APIVersion: alter.APIVersion,
		Kind:       alter.Kind,
		Metadata: alter.Metadata{
			ID:   "urn:axf:alter:00000000-0000-4000-8000-000000000000",
			Name: "programmatic",
			Owner: &alter.Owner{
				KeyType:              "ed25519",
				PublicKeyFingerprint: "sha256:0000000000000000000000000000000000000000000000000000000000000",
			},
			Extensions: alter.Extensions{"x-doolta-team": json.RawMessage(`"systems"`)},
		},
		Extensions: alter.Extensions{"x-doolta-source": json.RawMessage(`"unit-test"`)},
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := alter.Validate(encoded); err != nil {
		t.Fatalf("programmatically built document does not validate: %v\n%s", err, encoded)
	}

	reparsed, err := alter.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := string(reparsed.Extensions["x-doolta-source"]); got != `"unit-test"` {
		t.Errorf("root extension = %s, want \"unit-test\"", got)
	}
	if got := string(reparsed.Metadata.Extensions["x-doolta-team"]); got != `"systems"` {
		t.Errorf("metadata extension = %s, want \"systems\"", got)
	}
}

// Extensions must never shadow a schema field: a typed value is the source of
// truth, otherwise a stale extension entry could silently rewrite the document.
func TestMarshalDoesNotLetExtensionsOverwriteKnownFields(t *testing.T) {
	doc := alter.Alter{
		APIVersion: alter.APIVersion,
		Kind:       alter.Kind,
		Metadata:   alter.Metadata{ID: "urn:axf:alter:00000000-0000-4000-8000-000000000000", Name: "collide"},
		Extensions: alter.Extensions{"kind": json.RawMessage(`"Impostor"`)},
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded.Kind != alter.Kind {
		t.Errorf("kind = %q, want %q: an extension overwrote a schema field", decoded.Kind, alter.Kind)
	}
}

func TestUnmarshalRejectsMalformedNestedJSON(t *testing.T) {
	var doc alter.Alter
	if err := json.Unmarshal([]byte(`{"metadata": []}`), &doc); err == nil {
		t.Fatal("expected a decode error for a metadata array, got nil")
	}
}

func TestDefaultsOnAbsentFields(t *testing.T) {
	var nilPolicy *alter.Policy
	if !nilPolicy.RedactSecrets() {
		t.Error("nil policy: RedactSecrets() = false, want the schema default true")
	}
	if got := nilPolicy.EffectiveScope(); got != alter.ScopeGuard {
		t.Errorf("nil policy: EffectiveScope() = %q, want %q", got, alter.ScopeGuard)
	}
	var nilOwner *alter.Owner
	if got := nilOwner.EffectiveStatus(); got != alter.OwnerActive {
		t.Errorf("nil owner: EffectiveStatus() = %q, want %q", got, alter.OwnerActive)
	}

	explicitFalse := false
	policy := &alter.Policy{Audit: &alter.Audit{RedactSecrets: &explicitFalse}, Scope: alter.ScopeRuntime}
	if policy.RedactSecrets() {
		t.Error("explicit redactSecrets=false was overridden by the default")
	}
	if got := policy.EffectiveScope(); got != alter.ScopeRuntime {
		t.Errorf("EffectiveScope() = %q, want %q", got, alter.ScopeRuntime)
	}
	owner := &alter.Owner{Status: alter.OwnerRevoked}
	if got := owner.EffectiveStatus(); got != alter.OwnerRevoked {
		t.Errorf("EffectiveStatus() = %q, want %q", got, alter.OwnerRevoked)
	}
}

func TestCapabilityRegistryHoldsTheV0Names(t *testing.T) {
	want := []string{"shell", "browser-profile", "git-identity", "ssh-keypair", "locale", "ai-account"}
	if len(alter.CapabilityRegistry) != len(want) {
		t.Fatalf("registry = %v, want %v", alter.CapabilityRegistry, want)
	}
	for i, name := range want {
		if alter.CapabilityRegistry[i] != name {
			t.Errorf("registry[%d] = %q, want %q", i, alter.CapabilityRegistry[i], name)
		}
	}
}
