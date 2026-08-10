// Package conformance implements the AXF v0 rules that the JSON Schema cannot
// express, plus the SDK behaviour the conformance suite requires
// (spec section 17).
//
// It complements alter.Validate rather than replacing it: a document must pass
// schema validation first, then Check. The two are kept apart because the
// schema is the interoperable artifact shared with non-Go SDKs, while these
// rules are prose obligations each SDK re-implements.
package conformance

import (
	"errors"
	"fmt"

	alter "github.com/arhuman/axf"
)

// ErrDuplicateAssetName reports two assets sharing one name within a single
// Alter. Asset names address secrets, so a duplicate makes the reference
// ambiguous (spec section 10.3).
var ErrDuplicateAssetName = errors.New("conformance: duplicate asset name")

// ErrNilAlter reports a nil document passed to Check.
var ErrNilAlter = errors.New("conformance: nil alter")

// ParseAndCheck parses data with alter.Parse, then applies Check to the
// result: the two-step sequence, schema validation followed by the rules the
// schema cannot express, that every caller needs. It exists so a third caller
// reuses one named contract instead of re-deriving the same two lines.
func ParseAndCheck(data []byte) (*alter.Alter, error) {
	doc, err := alter.Parse(data)
	if err != nil {
		return nil, err
	}
	if err := Check(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Check applies the AXF v0 conformance rules that are not expressible in JSON
// Schema. It returns nil for a conformant Alter, or a joined error listing
// every violation found; use errors.Is against the exported sentinels to test
// for a specific rule.
//
// Check assumes the document already passed alter.Validate. It does not repeat
// structural checks the schema performs, such as requiring ownerSignature on
// every asset. A nil Alter is reported as an error, not a panic.
func Check(a *alter.Alter) error {
	if a == nil {
		return ErrNilAlter
	}
	var violations []error
	violations = append(violations, duplicateAssetNames(a.Assets)...)
	return errors.Join(violations...)
}

// duplicateAssetNames returns one error per asset that repeats an earlier name,
// so a caller sees every collision rather than only the first.
func duplicateAssetNames(assets []alter.Asset) []error {
	seen := make(map[string]int, len(assets))
	var violations []error
	for i, asset := range assets {
		if first, dup := seen[asset.Name]; dup {
			violations = append(violations, fmt.Errorf(
				"%w: %q at assets[%d] and assets[%d]", ErrDuplicateAssetName, asset.Name, first, i))
			continue
		}
		seen[asset.Name] = i
	}
	return violations
}

// Warnings returns the non-blocking advisories the AXF v0 SDK contract requires
// (spec sections 8, 11 and 17): an inline Asset held by an Alter without a
// recovery key fingerprint, and a deny policy that sets no explicit scope.
// Both stay warnings, never rejections: the document remains valid either way.
//
// Warnings returns nil for a nil Alter.
func Warnings(a *alter.Alter) []string {
	if a == nil {
		return nil
	}
	var warnings []string
	warnings = append(warnings, inlineAssetsWithoutRecoveryKey(a)...)
	warnings = append(warnings, denyPoliciesWithoutExplicitScope(a)...)
	return warnings
}

// inlineAssetsWithoutRecoveryKey warns on every inline Asset of an Alter that
// has no recovery key configured: losing the owner key makes such an Asset
// permanently undecryptable (spec section 8). A configured recovery key
// clears every inline Asset at once, since the recovery path covers the whole
// Alter rather than one Asset at a time.
func inlineAssetsWithoutRecoveryKey(a *alter.Alter) []string {
	if a.Metadata.Owner != nil && a.Metadata.Owner.RecoveryKeyFingerprint != "" {
		return nil
	}
	var warnings []string
	for _, asset := range a.Assets {
		if asset.Kind != alter.AssetKindInline {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"inline asset %q has no recovery key: metadata.owner.recoveryKeyFingerprint is unset, "+
				"so losing the owner key makes this asset permanently undecryptable", asset.Name))
	}
	return warnings
}

// denyPoliciesWithoutExplicitScope warns on every policies[] entry whose
// effect is deny and whose scope is unset. The schema default, guard, only
// observes and never blocks: a deny with no explicit scope reads as an
// enforced rule but silently does neither, unless the operator opts in with
// "scope": "runtime". The Alter Guard itself applies this default correctly
// (spec section 11); this warning exists because a plain reading of "effect:
// deny" invites the opposite assumption, and nothing else in the tooling
// flags the gap between the two.
func denyPoliciesWithoutExplicitScope(a *alter.Alter) []string {
	var warnings []string
	for i, policy := range a.Policies {
		if policy.Effect != alter.EffectDeny || policy.Scope != "" {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"policies[%d] denies {capability: %q, action: %q} but sets no explicit scope: it defaults "+
				"to scope \"guard\" (observe only) and does not block the action; add \"scope\": "+
				"\"runtime\" if blocking is intended", i, policy.Capability, policy.Action))
	}
	return warnings
}
