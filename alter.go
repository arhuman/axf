// Package alter implements the AXF (Alter eXtensible Format) v0 data model:
// the Go types mirroring the canonical JSON Schema, plus parsing and structural
// validation.
//
// The package deliberately stops at the data model. It performs no cryptography:
// Ed25519 signing/verification, age/JWE envelope encryption and the Key Event Log
// are runtime and SDK concerns layered on top of these types (see the AXF roadmap,
// spec section 23). Signature and encryption blocks are carried verbatim.
//
// Cross-field conformance rules that JSON Schema cannot express (asset name
// uniqueness, the inline-asset recovery-key warning) live in the sibling
// github.com/arhuman/axf/conformance package, not here.
package alter

import "encoding/json"

// Document-level constants. Both are fixed values in AXF v0.
const (
	// APIVersion is the only apiVersion accepted by this schema revision.
	APIVersion = "axf/v0"
	// Kind is the only document kind defined by AXF v0.
	Kind = "Alter"
)

// AssetKind discriminates how an Asset carries its value.
type AssetKind string

// Asset kinds. A ref Asset points at an external store and requires uri; an
// inline Asset carries an encrypted value and requires encryption.
const (
	AssetKindInline AssetKind = "inline"
	AssetKindRef    AssetKind = "ref"
)

// Effect is the decision a Policy expresses.
type Effect string

// Policy effects.
const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Scope separates enforcement from observability for a Policy.
type Scope string

// Policy scopes. ScopeRuntime blocks the action before it executes;
// ScopeGuard only observes and logs. ScopeGuard is the schema default.
const (
	ScopeRuntime Scope = "runtime"
	ScopeGuard   Scope = "guard"
)

// ConditionType names the runtime fact a Condition is evaluated against.
type ConditionType string

// Condition types. Extensible in later AXF revisions on the same model as the
// capability registry.
const (
	ConditionOS              ConditionType = "os"
	ConditionHostname        ConditionType = "hostname"
	ConditionEnv             ConditionType = "env"
	ConditionActiveAlterName ConditionType = "activeAlterName"
)

// Operator is the comparison a Condition applies to its value.
type Operator string

// Condition operators. equals/notEquals take a scalar value, in/notIn take an
// array; the schema does not enforce that pairing.
const (
	OperatorEquals    Operator = "equals"
	OperatorNotEquals Operator = "notEquals"
	OperatorIn        Operator = "in"
	OperatorNotIn     Operator = "notIn"
)

// AuditLevel is the verbosity of the audit trail a Policy requests.
type AuditLevel string

// Audit levels. AuditSummary is the schema default.
const (
	AuditNone     AuditLevel = "none"
	AuditSummary  AuditLevel = "summary"
	AuditDetailed AuditLevel = "detailed"
)

// OwnerStatus reflects the state of the owner key without consulting the
// external Key Event Log.
type OwnerStatus string

// Owner key statuses. OwnerActive is the schema default.
const (
	OwnerActive      OwnerStatus = "active"
	OwnerRevoked     OwnerStatus = "revoked"
	OwnerTransferred OwnerStatus = "transferred"
)

// KeyTypeEd25519 is the only owner key algorithm defined by AXF v0.
const KeyTypeEd25519 = "ed25519"

// CanonicalizationJCS is the only canonicalization scheme defined by AXF v0
// (JSON Canonicalization Scheme, RFC 8785).
const CanonicalizationJCS = "JCS-RFC8785"

// Encryption algorithms accepted for an inline Asset.
const (
	EncryptionAgeX25519 = "age-x25519"
	EncryptionJWE       = "jwe"
)

// CapabilityRegistry lists the canonical capability names of AXF v0. The list
// is open: a third-party capability must use an x-<vendor>-<capability> name
// until it is accepted into the registry. Membership is not validated by the
// schema, which accepts any string.
var CapabilityRegistry = []string{
	"shell",
	"browser-profile",
	"git-identity",
	"ssh-keypair",
	"locale",
	"ai-account",
}

// Alter is the root AXF document: a coherent digital environment representing
// one operational identity.
//
// Timestamps are kept as strings rather than time.Time so that a document
// round-trips byte-for-byte through Parse and json.Marshal: the content digest
// (spec section 16) is computed over canonical bytes, and reformatting an
// equivalent timestamp would change it.
type Alter struct {
	APIVersion   string       `json:"apiVersion"`
	Kind         string       `json:"kind"`
	Metadata     Metadata     `json:"metadata"`
	Context      *Context     `json:"context,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Assets       []Asset      `json:"assets,omitempty"`
	Policies     []Policy     `json:"policies,omitempty"`
	Lifecycle    *Lifecycle   `json:"lifecycle,omitempty"`
	Signature    *Signature   `json:"signature,omitempty"`

	// Extensions holds unknown x-<vendor> members found at this level and
	// re-emits them on marshal (spec section 13 round-tripping).
	Extensions Extensions `json:"-"`
}

// Metadata carries the stable identity of the Alter. ID is the immutable URN;
// Name is a mutable human label.
type Metadata struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Owner     *Owner `json:"owner,omitempty"`

	Extensions Extensions `json:"-"`
}

// Owner describes the key pair generated independently for this Alter only.
// It is never derived from a shared master key and never reused across distinct
// pseudonymous Alters: either would re-link them.
type Owner struct {
	KeyType              string `json:"keyType"`
	PublicKeyFingerprint string `json:"publicKeyFingerprint"`
	// RecoveryKeyFingerprint is optional. When it is empty, tooling must warn
	// on creation of an inline Asset (spec section 8).
	RecoveryKeyFingerprint string      `json:"recoveryKeyFingerprint,omitempty"`
	Status                 OwnerStatus `json:"status,omitempty"`
	// KeyEventLogURI references the external, independently signed log of
	// rotation/transfer/revocation/recovery events. Never embedded inline.
	KeyEventLogURI string `json:"keyEventLogUri,omitempty"`

	Extensions Extensions `json:"-"`
}

// Context holds environment settings shared by the Alter's capabilities.
type Context struct {
	Locale   string `json:"locale,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	// Custom is a free-form extension point. Values are kept as raw JSON so
	// unknown structures survive a decode/encode cycle unchanged.
	Custom map[string]json.RawMessage `json:"custom,omitempty"`

	Extensions Extensions `json:"-"`
}

// Capability is an abstract feature of the Alter. Provider is optional: the
// runtime, not the document, picks the concrete implementation.
type Capability struct {
	Type     string `json:"type"`
	Provider string `json:"provider,omitempty"`
	// Config is provider-agnostic configuration. Values are kept as raw JSON
	// so unknown structures survive a decode/encode cycle unchanged.
	Config map[string]json.RawMessage `json:"config,omitempty"`

	Extensions Extensions `json:"-"`
}

// Asset is a datum or secret attached to the Alter, either referenced in an
// external store (KindRef) or encrypted inline (KindInline).
//
// Name must be unique within one Alter. That rule is not expressible in the
// schema and is checked by the conformance package instead.
type Asset struct {
	Name       string      `json:"name"`
	Kind       AssetKind   `json:"kind"`
	URI        string      `json:"uri,omitempty"`
	Encryption *Encryption `json:"encryption,omitempty"`
	// OwnerSignature is required for every Asset, ref as well as inline: the
	// document signature excludes assets[], so without it a uri or a ciphertext
	// could be substituted by anyone with write access to the file.
	OwnerSignature *OwnerSignature `json:"ownerSignature"`

	Extensions Extensions `json:"-"`
}

// Encryption describes the envelope encryption of an inline Asset. Recipients
// holds the public keys the data encryption key was wrapped for: the owner key,
// the recovery key, and optionally other devices.
type Encryption struct {
	Algorithm  string   `json:"algorithm,omitempty"`
	Recipients []string `json:"recipients,omitempty"`
	Ciphertext string   `json:"ciphertext,omitempty"`

	Extensions Extensions `json:"-"`
}

// DetachedSignature is a signature stored beside the bytes it covers. The
// covered scope depends on where the signature sits: see Signature and
// OwnerSignature.
//
// This package never produces or verifies the Value; it only carries it.
type DetachedSignature struct {
	Algorithm        string `json:"algorithm"`
	Canonicalization string `json:"canonicalization"`
	SignedAt         string `json:"signedAt"`
	Value            string `json:"value"`

	Extensions Extensions `json:"-"`
}

// Signature is the document-level signature. It covers apiVersion, metadata
// (excluding updatedAt), context, capabilities, policies and lifecycle. It
// excludes assets[] and metadata.updatedAt.
type Signature = DetachedSignature

// OwnerSignature is an Asset-level detached signature over the canonicalized
// {name, kind, uri|encryption} tuple of that Asset, independent from the
// document-level Signature.
type OwnerSignature = DetachedSignature

// Policy governs the use of a Capability. Capability may name a capability the
// Alter does not itself declare, which is how a deny-by-default rule is written.
type Policy struct {
	Capability string     `json:"capability"`
	Action     string     `json:"action"`
	Effect     Effect     `json:"effect"`
	Condition  *Condition `json:"condition,omitempty"`
	Scope      Scope      `json:"scope,omitempty"`
	Audit      *Audit     `json:"audit,omitempty"`

	Extensions Extensions `json:"-"`
}

// Condition is a structured predicate, never a free-form expression string, so
// that every Alter Guard evaluates it identically.
type Condition struct {
	Type     ConditionType `json:"type"`
	Operator Operator      `json:"operator"`
	// Value is a scalar for equals/notEquals and an array for in/notIn. It is
	// kept as raw JSON because the schema deliberately leaves it untyped.
	Value json.RawMessage `json:"value"`
	// Name is the environment variable to inspect. Required when Type is
	// ConditionEnv; meaningless for every other condition type.
	Name string `json:"name,omitempty"`

	Extensions Extensions `json:"-"`
}

// Audit configures the trail produced when a Policy fires.
type Audit struct {
	Level AuditLevel `json:"level,omitempty"`
	// RedactSecrets is a pointer so an explicit false is distinguishable from
	// an absent field, whose schema default is true.
	RedactSecrets *bool `json:"redactSecrets,omitempty"`

	Extensions Extensions `json:"-"`
}

// Lifecycle declares what the runtime should do around activation and
// deactivation. It is declarative data; execution belongs to the runtime.
type Lifecycle struct {
	Hooks *Hooks `json:"hooks,omitempty"`

	Extensions Extensions `json:"-"`
}

// Hooks groups the four activation phases.
type Hooks struct {
	PreActivation    []Hook `json:"preActivation,omitempty"`
	PostActivation   []Hook `json:"postActivation,omitempty"`
	PreDeactivation  []Hook `json:"preDeactivation,omitempty"`
	PostDeactivation []Hook `json:"postDeactivation,omitempty"`

	Extensions Extensions `json:"-"`
}

// Hook is a {capability, action} pair, the same grammar as Policy targeting.
type Hook struct {
	Capability string `json:"capability"`
	Action     string `json:"action"`

	Extensions Extensions `json:"-"`
}

// RedactSecrets reports the effective value of audit.redactSecrets, applying
// the schema default (true) when the Policy or the field is absent.
func (p *Policy) RedactSecrets() bool {
	if p == nil || p.Audit == nil || p.Audit.RedactSecrets == nil {
		return true
	}
	return *p.Audit.RedactSecrets
}

// EffectiveScope reports the effective policy scope, applying the schema
// default (guard) when the field is absent.
func (p *Policy) EffectiveScope() Scope {
	if p == nil || p.Scope == "" {
		return ScopeGuard
	}
	return p.Scope
}

// EffectiveStatus reports the effective owner key status, applying the schema
// default (active) when the field is absent.
func (o *Owner) EffectiveStatus() OwnerStatus {
	if o == nil || o.Status == "" {
		return OwnerActive
	}
	return o.Status
}
