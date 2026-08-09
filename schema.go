package alter

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SchemaID is the canonical $id of the AXF v0 JSON Schema. The domain is a
// placeholder pending a decision on a neutral identity for the project
// (spec sections 22 and 24); the identifier itself is stable for v0.
const SchemaID = "https://axf.doolta.com/schema/v0/alter.schema.json"

//go:embed schema/alter.schema.json
var schemaJSON []byte

// ErrTrailingData reports JSON content after the end of the Alter document.
var ErrTrailingData = errors.New("alter: unexpected trailing data after JSON document")

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

// Schema returns a copy of the embedded JSON Schema source. Callers get a copy
// so they cannot mutate the schema every validation depends on.
func Schema() []byte {
	return slices.Clone(schemaJSON)
}

// compiledSchema compiles the embedded schema once. A failure here is a defect
// in this package, not in the caller's document.
func compiledSchema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft2020
		// Treat "format" as an assertion rather than an annotation: a malformed
		// date-time should fail validation, not pass silently.
		c.AssertFormat = true
		if err := c.AddResource(SchemaID, bytes.NewReader(schemaJSON)); err != nil {
			compileErr = fmt.Errorf("alter: loading embedded schema: %w", err)
			return
		}
		s, err := c.Compile(SchemaID)
		if err != nil {
			compileErr = fmt.Errorf("alter: compiling embedded schema: %w", err)
			return
		}
		compiled = s
	})
	return compiled, compileErr
}

// Validate checks raw JSON bytes against the embedded AXF v0 JSON Schema.
//
// It performs structural validation only. Cross-field conformance rules that
// JSON Schema cannot express (asset name uniqueness above all) are not checked
// here; use the conformance package for those.
func Validate(data []byte) error {
	schema, err := compiledSchema()
	if err != nil {
		return err
	}
	doc, err := decodeAny(data)
	if err != nil {
		return err
	}
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("alter: schema validation failed: %w", err)
	}
	return nil
}

// Parse validates raw JSON bytes against the embedded schema and decodes them
// into an Alter. Unknown x-<vendor> members are preserved in the Extensions
// field of the object that carried them.
//
// Parse returns an error for any document Validate rejects; it does not apply
// the conformance rules from the conformance package.
func Parse(data []byte) (*Alter, error) {
	if err := Validate(data); err != nil {
		return nil, err
	}
	var a Alter
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("alter: decoding document: %w", err)
	}
	return &a, nil
}

// decodeAny decodes one JSON value with numbers kept as json.Number, which the
// validator requires to compare numeric constraints without float rounding.
func decodeAny(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("alter: invalid JSON: %w", err)
	}
	if dec.More() {
		return nil, ErrTrailingData
	}
	return doc, nil
}
