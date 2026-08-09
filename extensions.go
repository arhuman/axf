package alter

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
)

// ExtensionPrefix is the namespace every third-party field must use. AXF v0
// accepts "x-<vendor>" members at every object level of the schema.
const ExtensionPrefix = "x-"

// Extensions holds the x-<vendor> members of one object, as raw JSON.
//
// A runtime that does not understand an extension must preserve it and ignore
// it, never reject the document (spec section 13). Every AXF struct therefore
// collects its unknown x-* members here on unmarshal and re-emits them on
// marshal. Values are raw JSON so that the exact bytes survive the round trip.
//
// Known fields always win: an Extensions entry whose key collides with a schema
// field is dropped on marshal rather than overwriting the typed value.
type Extensions map[string]json.RawMessage

var knownFieldCache sync.Map // reflect.Type -> map[string]struct{}

// knownJSONFields returns the set of JSON member names a struct type declares.
// Fields tagged json:"-" (the Extensions field itself) are excluded, which is
// what makes an x-* member "unknown" and therefore collectible.
func knownJSONFields(t reflect.Type) map[string]struct{} {
	if cached, ok := knownFieldCache.Load(t); ok {
		return cached.(map[string]struct{})
	}
	fields := make(map[string]struct{}, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = struct{}{}
	}
	knownFieldCache.Store(t, fields)
	return fields
}

// unmarshalExtensions decodes data into shadow (a pointer to a tag-identical
// type without marshaling methods, to avoid recursion) and collects every
// unknown x-* member into ext.
func unmarshalExtensions(data []byte, shadow any, ext *Extensions) error {
	if err := json.Unmarshal(data, shadow); err != nil {
		return err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	known := knownJSONFields(reflect.TypeOf(shadow).Elem())
	var found Extensions
	for name, raw := range members {
		if _, isKnown := known[name]; isKnown || !strings.HasPrefix(name, ExtensionPrefix) {
			continue
		}
		if found == nil {
			found = make(Extensions)
		}
		found[name] = raw
	}
	*ext = found
	return nil
}

// marshalExtensions encodes shadow and merges ext back in as sibling members.
func marshalExtensions(shadow any, ext Extensions) ([]byte, error) {
	encoded, err := json.Marshal(shadow)
	if err != nil {
		return nil, err
	}
	if len(ext) == 0 {
		return encoded, nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &members); err != nil {
		return nil, err
	}
	for name, raw := range ext {
		if _, collides := members[name]; collides {
			continue
		}
		members[name] = raw
	}
	return json.Marshal(members)
}

// UnmarshalJSON decodes an Alter, preserving unknown x-* members.
func (a *Alter) UnmarshalJSON(data []byte) error {
	type shadow Alter
	return unmarshalExtensions(data, (*shadow)(a), &a.Extensions)
}

// MarshalJSON encodes an Alter, re-emitting preserved x-* members.
func (a Alter) MarshalJSON() ([]byte, error) {
	type shadow Alter
	return marshalExtensions(shadow(a), a.Extensions)
}

// UnmarshalJSON decodes a Metadata, preserving unknown x-* members.
func (m *Metadata) UnmarshalJSON(data []byte) error {
	type shadow Metadata
	return unmarshalExtensions(data, (*shadow)(m), &m.Extensions)
}

// MarshalJSON encodes a Metadata, re-emitting preserved x-* members.
func (m Metadata) MarshalJSON() ([]byte, error) {
	type shadow Metadata
	return marshalExtensions(shadow(m), m.Extensions)
}

// UnmarshalJSON decodes an Owner, preserving unknown x-* members.
func (o *Owner) UnmarshalJSON(data []byte) error {
	type shadow Owner
	return unmarshalExtensions(data, (*shadow)(o), &o.Extensions)
}

// MarshalJSON encodes an Owner, re-emitting preserved x-* members.
func (o Owner) MarshalJSON() ([]byte, error) {
	type shadow Owner
	return marshalExtensions(shadow(o), o.Extensions)
}

// UnmarshalJSON decodes a Context, preserving unknown x-* members.
func (c *Context) UnmarshalJSON(data []byte) error {
	type shadow Context
	return unmarshalExtensions(data, (*shadow)(c), &c.Extensions)
}

// MarshalJSON encodes a Context, re-emitting preserved x-* members.
func (c Context) MarshalJSON() ([]byte, error) {
	type shadow Context
	return marshalExtensions(shadow(c), c.Extensions)
}

// UnmarshalJSON decodes a Capability, preserving unknown x-* members.
func (c *Capability) UnmarshalJSON(data []byte) error {
	type shadow Capability
	return unmarshalExtensions(data, (*shadow)(c), &c.Extensions)
}

// MarshalJSON encodes a Capability, re-emitting preserved x-* members.
func (c Capability) MarshalJSON() ([]byte, error) {
	type shadow Capability
	return marshalExtensions(shadow(c), c.Extensions)
}

// UnmarshalJSON decodes an Asset, preserving unknown x-* members.
func (a *Asset) UnmarshalJSON(data []byte) error {
	type shadow Asset
	return unmarshalExtensions(data, (*shadow)(a), &a.Extensions)
}

// MarshalJSON encodes an Asset, re-emitting preserved x-* members.
func (a Asset) MarshalJSON() ([]byte, error) {
	type shadow Asset
	return marshalExtensions(shadow(a), a.Extensions)
}

// UnmarshalJSON decodes an Encryption, preserving unknown x-* members.
func (e *Encryption) UnmarshalJSON(data []byte) error {
	type shadow Encryption
	return unmarshalExtensions(data, (*shadow)(e), &e.Extensions)
}

// MarshalJSON encodes an Encryption, re-emitting preserved x-* members.
func (e Encryption) MarshalJSON() ([]byte, error) {
	type shadow Encryption
	return marshalExtensions(shadow(e), e.Extensions)
}

// UnmarshalJSON decodes a DetachedSignature, preserving unknown x-* members.
func (s *DetachedSignature) UnmarshalJSON(data []byte) error {
	type shadow DetachedSignature
	return unmarshalExtensions(data, (*shadow)(s), &s.Extensions)
}

// MarshalJSON encodes a DetachedSignature, re-emitting preserved x-* members.
func (s DetachedSignature) MarshalJSON() ([]byte, error) {
	type shadow DetachedSignature
	return marshalExtensions(shadow(s), s.Extensions)
}

// UnmarshalJSON decodes a Policy, preserving unknown x-* members.
func (p *Policy) UnmarshalJSON(data []byte) error {
	type shadow Policy
	return unmarshalExtensions(data, (*shadow)(p), &p.Extensions)
}

// MarshalJSON encodes a Policy, re-emitting preserved x-* members.
func (p Policy) MarshalJSON() ([]byte, error) {
	type shadow Policy
	return marshalExtensions(shadow(p), p.Extensions)
}

// UnmarshalJSON decodes a Condition, preserving unknown x-* members.
func (c *Condition) UnmarshalJSON(data []byte) error {
	type shadow Condition
	return unmarshalExtensions(data, (*shadow)(c), &c.Extensions)
}

// MarshalJSON encodes a Condition, re-emitting preserved x-* members.
func (c Condition) MarshalJSON() ([]byte, error) {
	type shadow Condition
	return marshalExtensions(shadow(c), c.Extensions)
}

// UnmarshalJSON decodes an Audit, preserving unknown x-* members.
func (a *Audit) UnmarshalJSON(data []byte) error {
	type shadow Audit
	return unmarshalExtensions(data, (*shadow)(a), &a.Extensions)
}

// MarshalJSON encodes an Audit, re-emitting preserved x-* members.
func (a Audit) MarshalJSON() ([]byte, error) {
	type shadow Audit
	return marshalExtensions(shadow(a), a.Extensions)
}

// UnmarshalJSON decodes a Lifecycle, preserving unknown x-* members.
func (l *Lifecycle) UnmarshalJSON(data []byte) error {
	type shadow Lifecycle
	return unmarshalExtensions(data, (*shadow)(l), &l.Extensions)
}

// MarshalJSON encodes a Lifecycle, re-emitting preserved x-* members.
func (l Lifecycle) MarshalJSON() ([]byte, error) {
	type shadow Lifecycle
	return marshalExtensions(shadow(l), l.Extensions)
}

// UnmarshalJSON decodes a Hooks, preserving unknown x-* members.
func (h *Hooks) UnmarshalJSON(data []byte) error {
	type shadow Hooks
	return unmarshalExtensions(data, (*shadow)(h), &h.Extensions)
}

// MarshalJSON encodes a Hooks, re-emitting preserved x-* members.
func (h Hooks) MarshalJSON() ([]byte, error) {
	type shadow Hooks
	return marshalExtensions(shadow(h), h.Extensions)
}

// UnmarshalJSON decodes a Hook, preserving unknown x-* members.
func (h *Hook) UnmarshalJSON(data []byte) error {
	type shadow Hook
	return unmarshalExtensions(data, (*shadow)(h), &h.Extensions)
}

// MarshalJSON encodes a Hook, re-emitting preserved x-* members.
func (h Hook) MarshalJSON() ([]byte, error) {
	type shadow Hook
	return marshalExtensions(shadow(h), h.Extensions)
}
