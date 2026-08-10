# AXF: Alter eXtensible Format

**Status: Draft v0.** This is the reference specification for the AXF format. See [ARCHITECTURE.md](ARCHITECTURE.md) for how this repository's Go SDK, runtime and CLI implement it.

AXF is deliberately open. Publishing the format itself, not just a reference implementation, is what lets independent applications read, create, modify, exchange and run the same Alter without agreeing on anything beyond this document: openness here is in service of collaboration and interoperability across implementations, not any single runtime's convenience.

## 1. Vision

AXF is an open specification describing an Alter: a coherent digital environment representing an operational identity. An Alter gathers everything needed to act coherently and in isolation within a digital environment (browser, shell, SSH, Git, AI accounts, locale, history). The goal is to let several applications read, create, modify, exchange and run the same Alter without depending on any one implementation.

## 2. Motivation

A digital identity is today scattered across many tools, each with its own representation. AXF proposes a common model, comparable to the role OpenAPI plays for an API or OCI for a container: one reference spec, several independent implementations.

## 3. Principles

- **Alter First**: the format is the reference; applications are consumers of it.
- **Provider-based**: the format describes the what, runtimes choose the how.
- **Capability-oriented**: features are expressed as abstract capabilities.
- **Orthogonality**: each dimension (data, runtime, policies) evolves independently.
- **Selective encryption, never global**: an Alter stays readable and diffable (Git); only individual values marked sensitive are encrypted, never the whole document.
- **Identity scoped by Alter**: a key is never shared or derived from a source common to several distinct Alters. No data in the document may allow linking two Alters together, even indirectly (a derivation path, a reused fingerprint). This is a structuring principle that directly eliminated hierarchical key derivation (§7) in favor of independent keys per Alter.

## 4. Terminology

| Term | Definition |
|---|---|
| **Alter** | Document describing an operational identity: the subject of this spec. |
| **Runtime** | Implementation that interprets an Alter and executes it (`axf up <name>`). |
| **Provider** | Concrete implementation of a Capability for a given platform/tool (e.g. a Firefox provider for the `browser-profile` capability). Chosen by the runtime, never named in the format. |
| **Capability** | Feature expressed abstractly in the Alter (e.g. `shell`, `git-identity`). |
| **Asset** | Data or secret attached to the Alter, referenced externally or encrypted inline, individually signed by the owner. |
| **Policy** | Structured rule governing the use of a Capability, enforced or observed by the Alter Guard. |
| **Alter Guard** | Runtime component that applies Policies and produces an audit trail. Belongs to the runtime, not the format. |
| **Key Event Log** | External, signed artifact, distinct from the Alter document, that traces key rotations, transfers and revocations (§21). |

## 5. Canonical format

The canonical representation of an Alter is **JSON**, validated against a **JSON Schema draft 2020-12** schema. YAML is tolerated as a human-editing syntax, but must be converted to JSON before any strict validation or signature operation: the canonicalization required for signing (§9) is defined on JSON, not YAML, and keeping a single canonical form avoids serialization ambiguities that would break signature verification.

CUE or any other constraint language may be used as an advanced upstream validation tool, but is never the final exchange form: an artifact meant to be signed and verified bit-for-bit must stay in a format with bounded expressive power.

## 6. Document structure

```
Alter
├── apiVersion / kind
├── metadata            (stable id, human name, owner: independent key scoped to this Alter)
├── context              (locale, timezone, namespaced extensions)
├── capabilities[]        (type, optional provider, config)
├── assets[]              (kind: inline | ref, ownerSignature per entry)
├── policies[]            (capability, action, effect, structured condition, scope, audit)
├── lifecycle              (hooks {capability, action} pre/post activation-deactivation)
└── signature              (partial scope: excludes assets[] and updatedAt, reserved in V0)
```

## 7. Identity and owner key

### 7.1 Stable identifier
`metadata.id` is a stable URN (`urn:axf:alter:<uuid>`), distinct from `metadata.name` (a human label, mutable). It is the identifier content-digest versioning (§16), multi-device sync and the registry (V3) will rely on, so it must be decided as of V0 to avoid a later data migration.

### 7.2 Owner key
- Algorithm: **Ed25519** (short keys and signatures, no parameters to get wrong, already the default algorithm for modern SSH keys, which lets an existing key be reused).
- **Each Alter has its own key pair, generated independently.** There is no hierarchical derivation (BIP32 or equivalent) from a master key shared across Alters: a derivation path stored in the document would reveal that a set of Alters depends on a common source and would let them be correlated by the very structure of the path, even without ever exposing the master key. That would directly contradict the identity-scoped principle (§3). The accepted tradeoff: no simplified backup via a single master key, compensated by the per-Alter recovery mechanism (§8).
- `metadata.owner.publicKeyFingerprint` identifies the key of *this* Alter, never a global identity key reused across other Alters.
- Signature verification is not mandatory for a V0/V1 runtime, but the `signature` block must exist in the schema from now on so as not to break compatibility once enforcement arrives (V3).

## 8. Recovery: recovery key and code

Losing the owner key, with no safety net, makes every `inline` Asset permanently undecryptable. AXF provides a recovery mechanism, itself scoped per Alter so as not to reintroduce the correlation just eliminated in §7.2: a single recovery key shared across all of a user's Alters would be as dangerous as a master derivation key, for the same reason.

- An **independent** recovery key is generated when the Alter is created, encoded as a **BIP39**-style mnemonic seed (24 words): printable, eye-verifiable (built-in checksum), with no hierarchical derivation shared with another Alter.
- The seed is **never stored in the document**. It is displayed once at creation for offline printing/storage, then forgotten by the runtime.
- Only the public fingerprint derived from the seed is stored: `metadata.owner.recoveryKeyFingerprint`.
- The recovery key is added by default as a recipient of every `inline` Asset (`assets[].encryption.recipients[]`, §10), on the same footing as the owner key.
- **Optional**: an Alter may exist with no recovery key configured. In that case, the SDK/tooling **must emit an explicit warning** as soon as an `inline` Asset is created with no `recoveryKeyFingerprint` set, rather than blocking creation.
- Recovery itself is a `type: "recovery"` event of the Key Event Log (§21.2), signed by the recovery key rather than the old owner key. It generates a new owner pair **and** a new recovery code: the old code is considered consumed, like a single-use backup code.

## 9. Document signature

- Envelope: **JWS** (JSON Web Signature, RFC 7515), stored in the document's `signature` block.
- Canonicalization before signing: **JCS** (JSON Canonicalization Scheme, RFC 8785).
- **Partial scope, not the whole document**: the signature covers `apiVersion`, `metadata` (excluding `updatedAt`), `context`, `capabilities`, `policies` and `lifecycle`. It excludes `assets[]` and `metadata.updatedAt`.
- This exclusion is deliberate: every Asset ciphertext is already self-authenticated by its own AEAD tag (ChaCha20-Poly1305/GCM), and above all every Asset now carries its own independent signature (`ownerSignature`, §10). Including `assets[]` in the global signature would force a full new signing event on every secret rotation or recipient addition, when those are operations that must stay routine. The document signature only re-authenticates "who authorized these capabilities, these policies, this identity", which changes rarely and deserves an explicit signing event on every change.
- Verification is not mandatory for a V0/V1 runtime (§7.2), but the block is reserved in the schema from now on.

## 10. Assets and secrets

Two Asset natures coexist; neither is eliminated in favor of the other:

- **`ref`**: a pointer to an already-managed external store (Keychain, 1Password, Vault, SSH agent). Recommended by default for any long-lived or high-impact secret (a master SSH key, root credentials).
- **`inline`**: a value encrypted directly in the document, via **envelope encryption**: a disposable symmetric key (DEK) encrypts the value, and the DEK is itself encrypted against one or more recipient public keys (`assets[].encryption.recipients[]`). Recommended format: **age** (X25519 + ChaCha20-Poly1305, native Go implementation), alternative **JWE** if JOSE interoperability is required. Reserved for short, rotatable secrets dedicated to this Alter.

Encryption is **always selective, per Asset**: never encryption of the whole document. A globally encrypted Alter loses its Git diffability and becomes impossible for a third party to audit structurally.

No secret must ever appear in the clear in the document, whatever its nature.

### 10.1 Multiple recipients
`assets[].encryption.recipients[]` accepts several public keys from V0 onward (owner, recovery key §8, possibly other devices). This is an encryption mechanism available now, managed manually: the user adds a second device's public fingerprint themselves. Only automatic **discovery** and **synchronization** between devices are deferred to V3 (§22); the cryptographic mechanism itself does not change between V0 and V3.

### 10.2 Integrity and per-Asset signature
Since the document signature excludes `assets[]` (§9), every `assets[]` entry carries its own detached signature: `ownerSignature`, computed by the owner key (or the recovery key during a recovery operation) over the canonicalized `{name, kind, uri|encryption}` tuple of that entry. Without this mechanism, someone with mere write access to the file (a co-contributor on a shared Git repository, say) could silently substitute an entire Asset (replace an SSH key or a token with a value they control) without invalidating the document's global signature, since that signature does not cover `assets[]`. AEAD authentication of the ciphertext protects against *modification*, not against *substitution*: `ownerSignature` closes that gap by also covering `encryption.recipients[]`, which at the same time prevents an unauthorized recipient from being added.

`ownerSignature` is required for every `assets[]` entry, `ref` as much as `inline`: substituting a `uri` (redirecting a reference to a secret controlled by a third party) is just as valid an attack as substituting a ciphertext.

### 10.3 Uniqueness
`assets[].name` must be unique within a single Alter. This constraint is not expressed in the JSON Schema (hard to express cleanly in plain JSON Schema for a uniqueness property across items of an array) but is part of the conformance rules (§17): an Alter with two Assets of the same name is an invalid test case.

Targeting a Policy at a specific Asset (beyond targeting by Capability, §11) is explicitly out of scope for V0 (§22): the real need is not yet established by usage, and fixing its shape now would risk not matching the patterns that emerge in V1/V2.

## 11. Policies

Policies are structured expressions, never free prose, so that they remain identically implementable by any Alter Guard:

- `capability` / `action`: the rule's target. `capability` **may** reference a capability the Alter does not itself declare in `capabilities[]`: a legitimate use case for a defensive deny-by-default rule (e.g. forbidding any browser launch on an Alter that was never meant to have one), without forcing an artificial capability declaration purely to forbid it.
- `effect`: `allow` or `deny`.
- `condition`: a structured object, never a free string evaluated by the runtime: `{ "type": "os" | "hostname" | "env" | "activeAlterName", "operator": "equals" | "notEquals" | "in" | "notIn", "value": ..., "name": "..." }`. Extensible by adding new `type` values over time, on the same model as the capability registry (§14). `name` is required only when `type` is `env`: it names the environment variable to inspect (without it, `env` is not evaluable, a blocking conformance error rather than an ignored optional field). `activeAlterName` compares the activation name used by the runtime (the argument passed to `axf up <name>`, a runtime convention), not `metadata.id`: the two are distinct by construction (§7.1), and comparing a URN would be both impractical to type/read in a shell and not guaranteed to be stable across runtimes.
- `scope`: `runtime` (blocks the action before it executes) or `guard` (observes and logs without blocking). This distinction separates enforcement from observability, two different needs for the same Alter.
- `audit.redactSecrets`: guarantees a decrypted secret never appears in an audit log, even when the policy triggering the event concerns an Asset.

## 12. Lifecycle

The format reserves a `lifecycle.hooks` block (pre-activation, post-activation, pre-deactivation, post-deactivation), each hook a structured pair `{ "capability": "...", "action": "..." }`, the same pattern as `policies[].capability`/`action` (§11), so as not to reinvent a second ad hoc grammar to express the same idea. This is declarative data, not behavior: it belongs to the format even though its execution is carried by the runtime (V1). Without this block, nothing lets a runtime know how to clean up a previous Alter's state on `axf up <other-alter>`, when switching between pseudonyms is the format's central use case.

An Alter's effective runtime state (active, inactive, suspended) is not stored in the document itself: it is a runtime execution state, not format data.

## 13. Extensibility

Any extension not covered by the core schema goes through an `x-<vendor>` namespace. **Uniform rule**: every schema object that declares `additionalProperties: false` must also systematically declare `"patternProperties": {"^x-[a-z0-9-]+$": {}}`, at the document root, but also in `metadata`, `metadata.owner`, `context`, each `capabilities[]` item, each `assets[]` item (and its `encryption` sub-object), each `policies[]` item (and its `condition`/`audit` sub-objects), `lifecycle` (and `lifecycle.hooks`), and `signature`. This rule is applied systematically rather than decided case by case, so that a future schema extension does not reintroduce the same inconsistency by omission.

A runtime that does not recognize an `x-*` field must preserve and ignore it, never reject the document (round-tripping). Core versioning (`apiVersion`) is independent of each extension's own versioning.

## 14. Capability registry (V0, open)

Initial list of canonical names, extensible without revising the core schema:

`shell`, `browser-profile`, `git-identity`, `ssh-keypair`, `locale`, `ai-account`.

Every third-party capability must be exposed under a namespaced name (`x-<vendor>-<capability>`) until it has been proposed and accepted into the canonical registry.

## 15. Audit

Events produced by the Alter Guard follow a standard schema, independent of the runtime emitting them:

```json
{
  "timestamp": "2026-08-09T10:00:00Z",
  "alterId": "urn:axf:alter:6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e",
  "actor": "virtual-process-id",
  "capability": "browser-profile",
  "action": "launch",
  "provider": "firefox",
  "result": "denied"
}
```

`actor` references a virtual process identifier, never the host's real PID: an audit that exposed real system identifiers would itself become a leak vector for the pseudonymity the Alter is meant to protect.

This audit trail covers Capability actions. Key rotation/transfer/revocation events follow a separate, signed trail, the Key Event Log (§21.2), which is not produced by the Alter Guard.

## 16. Versioning

An Alter's content is identified by a **digest** (content-addressing, OCI-manifest style). This digest is **never a field stored in the document**: a self-referential field would pose the same bootstrap problem as the signature (it would have to be excluded from its own computation). It is computed **externally**, over the JCS canonical bytes of the **entire** document, **with no exclusion at all** (unlike the signature's partial scope, §9), and serves as a content-addressable key in storage/registry, with a mutable "current" pointer per `metadata.id`.

The digest and the signature have different scopes because they serve different needs: the digest identifies a version for diff/rollback/sync (an Asset rotation *is* a new version, so it must change the digest), the signature only re-authenticates the declarative identity fields (§9), which change rarely. This mechanism serves as the basis for diff/rollback (V0/V1), multi-device sync and the registry in V3, without needing a later redesign.

## 17. Conformance

Two levels of conformance suite are required from V0 onward, not just a schema validator:

- **Format**: a set of fixtures (valid/invalid Alters) that every parser/SDK implementation must pass, including at minimum: two Assets sharing the same `name` in the same Alter (invalid, §10.3), an `x-*` field accepted at every schema level listed in §13, an `inline` Asset with no `ownerSignature` (invalid, §10.2).
- **SDK behavior**: beyond the structural fixtures, the SDK must emit a warning (not a blocking error) when creating an `inline` Asset with no `recoveryKeyFingerprint` configured (§8).
- **Provider**: a test contract every provider must satisfy to claim it implements a capability from the canonical registry (§14), so that two competing providers for `browser-profile` remain interchangeable from the Alter's point of view.

## 18. Canonical instance example

```json
{
  "apiVersion": "axf/v0",
  "kind": "Alter",
  "metadata": {
    "id": "urn:axf:alter:6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e",
    "name": "systems-alchemist",
    "createdAt": "2026-08-09T10:00:00Z",
    "updatedAt": "2026-08-09T10:00:00Z",
    "owner": {
      "keyType": "ed25519",
      "publicKeyFingerprint": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a0",
      "recoveryKeyFingerprint": "sha256:1b2a3c4d5e6f7089a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5",
      "status": "active",
      "keyEventLogUri": "ref://axf-registry/alter/6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e/key-events"
    }
  },
  "context": {
    "locale": "fr-FR",
    "timezone": "Europe/Paris",
    "custom": {
      "x-doolta": { "team": "systems" }
    }
  },
  "capabilities": [
    {
      "type": "git-identity",
      "config": { "name": "Alchemist", "email": "alchemist@example.com" }
    },
    {
      "type": "shell",
      "config": { "promptLabel": "alchemist" }
    }
  ],
  "assets": [
    {
      "name": "github-token",
      "kind": "ref",
      "uri": "keychain://axf/github-token",
      "ownerSignature": {
        "algorithm": "ed25519",
        "canonicalization": "JCS-RFC8785",
        "signedAt": "2026-08-09T10:00:00Z",
        "value": "base64:MEQCIBx7...=="
      }
    },
    {
      "name": "ssh-key",
      "kind": "inline",
      "encryption": {
        "algorithm": "age-x25519",
        "recipients": ["age1qz3s...owner", "age1qz3s...recovery"],
        "ciphertext": "YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+IFgyNTUxOSB..."
      },
      "ownerSignature": {
        "algorithm": "ed25519",
        "canonicalization": "JCS-RFC8785",
        "signedAt": "2026-08-09T10:00:00Z",
        "value": "base64:MEUCIQC91z...=="
      }
    }
  ],
  "policies": [
    {
      "capability": "browser-profile",
      "action": "launch",
      "effect": "deny",
      "scope": "runtime",
      "condition": {
        "type": "activeAlterName",
        "operator": "notEquals",
        "value": "systems-alchemist"
      },
      "audit": { "level": "detailed", "redactSecrets": true }
    }
  ],
  "lifecycle": {
    "hooks": {
      "postActivation": [ { "capability": "shell", "action": "start" } ],
      "preDeactivation": [ { "capability": "ssh-keypair", "action": "flush" } ]
    }
  },
  "signature": {
    "algorithm": "ed25519",
    "canonicalization": "JCS-RFC8785",
    "signedAt": "2026-08-09T10:00:05Z",
    "value": "base64:MEUCIQDx9...=="
  }
}
```

## 19. JSON Schema (draft 2020-12)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://axf.doolta.com/schema/v0/alter.schema.json",
  "title": "AXF Alter",
  "type": "object",
  "required": ["apiVersion", "kind", "metadata"],
  "$defs": {
    "hook": {
      "type": "object",
      "required": ["capability", "action"],
      "properties": {
        "capability": { "type": "string" },
        "action": { "type": "string" }
      },
      "patternProperties": { "^x-[a-z0-9-]+$": {} },
      "additionalProperties": false
    },
    "detachedSignature": {
      "type": "object",
      "required": ["algorithm", "canonicalization", "signedAt", "value"],
      "properties": {
        "algorithm": { "enum": ["ed25519"] },
        "canonicalization": { "enum": ["JCS-RFC8785"] },
        "signedAt": { "type": "string", "format": "date-time" },
        "value": { "type": "string" }
      },
      "patternProperties": { "^x-[a-z0-9-]+$": {} },
      "additionalProperties": false
    }
  },
  "properties": {
    "apiVersion": { "const": "axf/v0" },
    "kind": { "const": "Alter" },
    "metadata": {
      "type": "object",
      "required": ["id", "name", "owner"],
      "properties": {
        "id": { "type": "string", "pattern": "^urn:axf:alter:[0-9a-f-]{36}$" },
        "name": { "type": "string", "minLength": 1 },
        "createdAt": { "type": "string", "format": "date-time" },
        "updatedAt": { "type": "string", "format": "date-time" },
        "owner": {
          "type": "object",
          "description": "Key generated independently for THIS Alter only. Never derived from a shared master key and never reused across distinct pseudonymous Alters: either would re-link them.",
          "required": ["keyType", "publicKeyFingerprint"],
          "properties": {
            "keyType": { "enum": ["ed25519"] },
            "publicKeyFingerprint": { "type": "string" },
            "recoveryKeyFingerprint": {
              "type": "string",
              "description": "Optional. Fingerprint of an independent, Alter-scoped recovery key (BIP39 mnemonic generated once at creation, never persisted). If absent, tooling MUST warn when creating an inline Asset."
            },
            "status": {
              "enum": ["active", "revoked", "transferred"],
              "default": "active",
              "description": "MUST be readable without consulting the key event log."
            },
            "keyEventLogUri": {
              "type": "string",
              "description": "Reference to the external, independently signed log of rotation/transfer/revocation/recovery events. Never embedded inline."
            }
          },
          "patternProperties": { "^x-[a-z0-9-]+$": {} },
          "additionalProperties": false
        }
      },
      "patternProperties": { "^x-[a-z0-9-]+$": {} },
      "additionalProperties": false
    },
    "context": {
      "type": "object",
      "properties": {
        "locale": { "type": "string" },
        "timezone": { "type": "string" },
        "custom": { "type": "object" }
      },
      "patternProperties": { "^x-[a-z0-9-]+$": {} },
      "additionalProperties": false
    },
    "capabilities": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["type"],
        "properties": {
          "type": { "type": "string" },
          "provider": { "type": "string" },
          "config": { "type": "object" }
        },
        "patternProperties": { "^x-[a-z0-9-]+$": {} },
        "additionalProperties": false
      }
    },
    "assets": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name", "kind", "ownerSignature"],
        "description": "assets[].name MUST be unique within this Alter (conformance rule, not expressible as a plain structural constraint).",
        "properties": {
          "name": { "type": "string" },
          "kind": { "enum": ["inline", "ref"] },
          "uri": { "type": "string" },
          "encryption": {
            "type": "object",
            "properties": {
              "algorithm": { "enum": ["age-x25519", "jwe"] },
              "recipients": { "type": "array", "items": { "type": "string" } },
              "ciphertext": { "type": "string" }
            },
            "patternProperties": { "^x-[a-z0-9-]+$": {} },
            "additionalProperties": false
          },
          "ownerSignature": {
            "allOf": [{ "$ref": "#/$defs/detachedSignature" }],
            "description": "Detached signature over the canonicalized {name, kind, uri|encryption} tuple of this asset. Independent from the document-level signature, which excludes assets[]. Prevents silent substitution of ciphertext, uri, or recipients by anyone with mere write access to the file."
          }
        },
        "patternProperties": { "^x-[a-z0-9-]+$": {} },
        "additionalProperties": false,
        "allOf": [
          {
            "if": { "properties": { "kind": { "const": "ref" } } },
            "then": { "required": ["uri"] }
          },
          {
            "if": { "properties": { "kind": { "const": "inline" } } },
            "then": { "required": ["encryption"] }
          }
        ]
      }
    },
    "policies": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["capability", "action", "effect"],
        "properties": {
          "capability": {
            "type": "string",
            "description": "MAY reference a capability not declared in this Alter's own capabilities[] (e.g. a defensive deny-by-default rule)."
          },
          "action": { "type": "string" },
          "effect": { "enum": ["allow", "deny"] },
          "condition": {
            "type": "object",
            "description": "Structured condition. Not a free-form expression string.",
            "required": ["type", "operator", "value"],
            "properties": {
              "type": { "enum": ["os", "hostname", "env", "activeAlterName"] },
              "operator": { "enum": ["equals", "notEquals", "in", "notIn"] },
              "value": { "description": "Scalar for equals/notEquals, array for in/notIn." },
              "name": { "type": "string", "description": "Required when type is 'env': the environment variable to inspect. Meaningless for other types." }
            },
            "patternProperties": { "^x-[a-z0-9-]+$": {} },
            "additionalProperties": false,
            "allOf": [
              {
                "if": { "properties": { "type": { "const": "env" } } },
                "then": { "required": ["name"] }
              }
            ]
          },
          "scope": {
            "enum": ["runtime", "guard"],
            "default": "guard",
            "description": "runtime: enforced before the action executes. guard: observed/logged only, does not block."
          },
          "audit": {
            "type": "object",
            "properties": {
              "level": { "enum": ["none", "summary", "detailed"], "default": "summary" },
              "redactSecrets": { "type": "boolean", "default": true }
            },
            "patternProperties": { "^x-[a-z0-9-]+$": {} },
            "additionalProperties": false
          }
        },
        "patternProperties": { "^x-[a-z0-9-]+$": {} },
        "additionalProperties": false
      }
    },
    "lifecycle": {
      "type": "object",
      "properties": {
        "hooks": {
          "type": "object",
          "properties": {
            "preActivation": { "type": "array", "items": { "$ref": "#/$defs/hook" } },
            "postActivation": { "type": "array", "items": { "$ref": "#/$defs/hook" } },
            "preDeactivation": { "type": "array", "items": { "$ref": "#/$defs/hook" } },
            "postDeactivation": { "type": "array", "items": { "$ref": "#/$defs/hook" } }
          },
          "patternProperties": { "^x-[a-z0-9-]+$": {} },
          "additionalProperties": false
        }
      },
      "patternProperties": { "^x-[a-z0-9-]+$": {} },
      "additionalProperties": false
    },
    "signature": {
      "allOf": [{ "$ref": "#/$defs/detachedSignature" }],
      "description": "Covers apiVersion, metadata (excluding updatedAt), context, capabilities, policies and lifecycle. Excludes assets[] (each Asset carries its own ownerSignature instead) and metadata.updatedAt."
    }
  },
  "patternProperties": {
    "^x-[a-z0-9-]+$": {}
  },
  "additionalProperties": false
}
```

## 20. Anonymity and pseudonymity

As in the initial vision, AXF does not aim to guarantee absolute anonymity or to circumvent legal obligations (KYC, taxation, regulation). Anonymity remains an emergent property of a correctly designed Alter run by a compatible runtime. This V0's decisions (independent per-Alter keys with no shared derivation, a recovery key itself scoped per Alter, audit with no real PID, selective encryption, per-Asset signature) specifically reduce the correlation and substitution vectors between distinct Alters identified during evaluation, but do not constitute a formal guarantee.

## 21. Key rotation, ownership transfer, revocation

These operations are not mutations of the Alter document: they are trust events that must remain verifiable even when the local copy of the Alter is stale or cached. The format carries references to these events, not their mechanism.

### 21.1 In the Alter document (data readable without executing anything)
- `metadata.owner.status`: `active`, `revoked` or `transferred`. Must be directly readable, per the Alter First principle.
- `metadata.owner.recoveryKeyFingerprint`: optional, see §8.
- `metadata.owner.keyEventLogUri`: external reference to the key event log. The Alter document never contains the full history, only the pointer.

### 21.2 Outside the document, in a separate signed artifact: the Key Event Log
Every rotation, transfer, revocation **or recovery** (§8) is an independent signed event, never a silent rewrite of the main document. An ordinary rotation/transfer/revocation event is signed by the previous owner key, which preserves the continuity of trust (the TUF root-metadata / PGP revocation-certificate model). A `recovery`-type event is the exception: it is signed by the recovery key, precisely because the previous owner key is by hypothesis lost.

This artifact is externalized, not permanently embedded in the Alter: the document must stay small and stable, and revocation must remain verifiable even if the Alter's local cache is stale, exactly as a CRL/OCSP distribution point remains reachable independently of the certificate it revokes.

### 21.3 Rotation cadence and authorization: reuses existing Policies
No new schema block is needed: `policies[]` with `capability: "identity"` and `action: "rotate"` or `"transfer"` governs who may trigger the operation and how often, enforced by the Alter Guard like any other capability.

### 21.4 Ownership transfer: specific normative constraints
A transfer is not just a key change: every Asset has been signed (`ownerSignature`, §10.2) and every `inline` Asset has been encrypted against the old owner key. The SDK/runtime performing a transfer **must**, before publishing the transfer event to the Key Event Log:
1. re-encrypt every `inline` Asset against the new owner key,
2. re-sign (`ownerSignature`) every `assets[]` entry with the new key,
3. re-sign the document itself (§9) with the new key, since `metadata.owner` is part of the signed scope.

Without these three steps, the new owner inherits an Alter whose secrets they can neither decrypt nor authenticate.

### 21.5 Revocation distribution and discovery
The concrete verification mechanism (where and how a runtime consults revocation state) depends on the registry and stays out of scope for V0 (§22). Only the `metadata.owner.keyEventLogUri` and `metadata.owner.recoveryKeyFingerprint` pointers need to exist in the schema from V0 onward, so as not to break compatibility once the mechanism is specified.

## 22. Out of scope for V0 (explicitly deferred)

- Mandatory signature verification by the runtime (reserved in the schema, enforced in V3).
- **Automatic** multi-device registry/synchronization (V3): the multi-recipient encryption mechanism itself is available from V0 onward, managed manually (§10.1).
- Exact format and protocol of the Key Event Log (§21.2): the pointers are reserved from V0 onward, the mechanism (event structure, distribution, verification) is specified in V2/V3.
- Targeting a Policy at a specific Asset (beyond targeting by Capability): deferred to V2, the real need not yet established by usage (§10.3).
- Automatic provider negotiation when several providers are available for the same capability (resolved manually/by local config in V0/V1).
- LSP, a visual editor, distribution via OCI registries: important adoption levers but not normative for the format itself.
- Final domain for the schema `$id`: see §24. The schema currently lives at `https://axf.doolta.com/schema/v0/alter.schema.json` and will stay there until adoption requires a more neutral location, independent of any single person's or company's domain.

## 23. Roadmap (updated)

- **V0**: this document (data model, schema, parser, validator, Go SDK), format conformance suite (§17).
- **V1**: Runtime (Browser, Shell, Linux, macOS), execution of `lifecycle.hooks` (§12).
- **V1.1**: Alter Guard (enforcement of `policies[].scope: runtime`, audit conforming to the schema in §15).
- **V2**: new capabilities (Git, SSH, Mail, AI, Network), extending the registry (§14), provider conformance suite, Key Event Log specification (§21.2), Policy targeting by Asset.
- **V3+**: mandatory signature verification, distributed registry via OCI artifacts, automatic multi-device discovery/sync, provider marketplace.

## 24. Governance and meta (project, not format)

These decisions concern the AXF project itself, not the technical spec: they do not belong in the preceding sections, but they shape how the project starts.

- **Name**: the format/spec is called **AXF** (already consistent throughout the technical layer: `urn:axf:alter:...`, `apiVersion: "axf/v0"`). **Alter** remains the conceptual term for an instance. The CLI binary is called **`axf`**, not `alter`: "alter" is too generic an English word (risk of collision and poor indexing) to carry a command a user literally types every day.
- **Schema identifier**: `https://axf.doolta.com/schema/v0/alter.schema.json`. This is an immediately real, already-owned domain rather than a placeholder pointing nowhere, chosen over `axf.dev` (confirmed taken by a third party) or `axf.com` (status unverified). It will stay the schema's `$id` until AXF's adoption grows enough to need a neutral identity independent of any single person's or company's brand; that move, if and when it happens, is meant to be a deliberate, single, well-announced event, not something done casually once implementations already depend on this URL.
- **License**: **MIT**, consistent with the same author's other projects (mnemos, prooflog, minexus). Apache-2.0 (an explicit patent clause, useful if corporate contributors join the project) remains a migration option before a stable v1.0, not a choice to settle now.
- **Repository structure and governance**: a monorepo (spec, schema, fixtures, Go SDK together) and no formal proposal process (RFC-like) while the project stays solo. Two explicit triggers will prompt revisiting this choice: a second SDK language arriving, or the project's first external contributor. Before that, the decision trail behind this document's choices already serves as a decision log.
