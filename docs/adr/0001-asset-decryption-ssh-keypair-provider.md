---
status: accepted
date: 2026-08-10
---

# Asset decryption and the ssh-keypair provider

## Context

The v0 spec (section 10) defines `inline` Assets as envelope-encrypted values:
a data encryption key wraps the secret, itself wrapped for one or more
recipient public keys (`assets[].encryption.recipients[]`), recommended
algorithm `age-x25519`. This SDK performs no cryptography yet (see the
`alter` package doc comment and the runtime v1 doc comment): `ssh-keypair` and
`ai-account` have no provider because decrypting an inline Asset requires a
locally held private key the runtime has no way to load, and `Provider.Activate`
has no access to `doc.Assets` in the first place.

Two things block `ssh-keypair` specifically:

1. Where a local decryption identity (the private counterpart of an
   `encryption.recipients[]` entry) lives on disk.
2. How a `Provider` reaches the Asset its capability's config names, since
   `Target` today carries only `Name` and `Home`.

This ADR scopes the v2 slice to `ssh-keypair` only. `ai-account` is deferred:
it needs the same decryption plumbing but its own provider shape (likely
writing credentials to a tool-specific config rather than a key file), and is
not part of this pass.

## Decision

**Decryption library**: `filippo.io/age`. It is the reference Go
implementation of the algorithm the spec already names, has no unsafe-default
footguns, and its identity/recipient text format is exactly what
`assets[].encryption.recipients[]` already stores as bech32 strings.

**Identity storage**: `$AXF_HOME/keys/<alterName>.age`, one age identity file
(the plain `age-keygen` text format: comment lines plus one
`AGE-SECRET-KEY-1...` line) per Alter store name, directory mode `0o700`, file
mode `0o600`. Scoped per Alter name rather than one global identity, for the
same reason the owner and recovery keys are scoped per Alter (spec section
3's Alter-scoped identity principle): a single shared decryption identity across
Alters would let anything holding it correlate them. A new `axf keys generate
<name>` subcommand creates one and prints its recipient (`age1...`) for the
user to add to `assets[].encryption.recipients[]` by hand; nothing generates
or edits Alter documents automatically.

**Asset reachability from a Provider**: add `Assets []alter.Asset` to
`Target`, populated by `Runtime.Up` and `Runtime.teardown` from `doc.Assets`.
This is additive: every existing `Provider` implementation ignores an unused
struct field, so `shell`, `git-identity` and `browser-profile` need no change.
The alternative, threading `*alter.Alter` itself into `Provider.Activate`,
was rejected: it would let a provider reach `policies[]` and `lifecycle[]`,
which is the Alter Guard's and the Runtime's job respectively, not a
Provider's.

**ssh-keypair config**: `{"asset": "<assets[].name>"}`, the name of the Asset
this capability activates. Required; missing or unresolvable is an
`Activate` error, following the git-identity precedent of failing on an
incomplete config rather than silently degrading (spec section 3,
Provider-based: the format names the capability, the config it carries is
what a provider requires to make sense of it).

**kind: ref vs kind: inline**: `ref` is treated as an existing private key
path already on disk (`assets[].uri`, validated to exist and be a regular
file), no decryption involved: this is the `Store`/`Keychain`-agnostic
posture consistent with `ref` elsewhere in the spec (a pointer to an
already-managed external store, section 10). `inline` is decrypted with the local
identity at `$AXF_HOME/keys/<alterName>.age` against
`assets[].encryption.recipients[]`, and the plaintext is written to
`$AXF_HOME/keys/ssh/<alterName>/<assetName>`, mode `0o600`, parent directory
`0o700`, mirroring the isolation `BrowserProfileProvider` already applies to
per-Alter sensitive state. Activate is idempotent: re-running re-decrypts and
overwrites the same deterministic path, matching the existing contract every
other provider already honours.

**Exported variable**: `AXF_SSH_KEY_PATH`, the resolved key path. Following
`AXF_BROWSER_PROFILE`: informational and scriptable, not a variable any
external tool reads automatically. Wiring it into `ssh`/`git` invocations
(`GIT_SSH_COMMAND`, `-F`) is left to the user's own shell config or a
lifecycle hook; it is not this provider's job to also mutate git's
configuration, which is `git-identity`'s and `GIT_SSH_COMMAND`'s separate
concern.

**Explicitly out of scope for this pass**: `ai-account` provider, asset
*creation*/encryption tooling (producing a valid `inline` Asset from a secret
is SDK work the `alter` package doc comment already defers), the Key Event
Log, signature verification, and Policy targeting by Asset. Test fixtures for
this change build their ciphertext directly with `filippo.io/age` in test
code, not through a CLI command this pass does not add.

## Consequences

### Positive
- `ssh-keypair` becomes usable end to end: generate an identity, add its
  recipient to an Asset, activate an Alter that references it.
- `Target.Assets` is a small, additive change that unblocks `ai-account` too
  without a second signature change later.
- Per-Alter identity scoping keeps the correlation-resistance property the
  rest of the spec already commits to.

### Negative
- A second, informal key material store (`$AXF_HOME/keys/`) exists alongside
  the Alter documents themselves; losing `$AXF_HOME` loses decryption
  identities the same way it would lose everything else there, with no
  additional backup story introduced by this ADR.
- No CLI path to actually produce a valid `inline` Asset yet, so exercising
  `ssh-keypair` end to end still requires hand-editing a document or writing
  Go against `filippo.io/age` directly.

### Risks
- If `ai-account` later needs a *different* identity-storage convention (for
  example one identity shared across capabilities within an Alter rather
  than the ssh-keypair-only assumption implicit here), `$AXF_HOME/keys/<alterName>.age`
  already generalizes to that: it is keyed by Alter, not by capability.

## Alternatives Considered

### Option 1: OS keychain (Keychain/libsecret) for the decryption identity
- Pros: no plaintext key material on disk, familiar to users of `ref` Assets.
- Cons: platform-specific, breaks the project's stated Linux/darwin parity for
  v1/v2, and age identities are designed to be plain files (`age-keygen`
  already writes one); reintroducing a second, inconsistent storage model for
  the same kind of secret was not worth it for this pass.

### Option 2: Thread `*alter.Alter` into `Provider.Activate` instead of `Target.Assets`
- Pros: one field to add instead of a new one; a provider could in principle
  read anything about the document.
- Cons: gives a Provider reach into `policies[]`/`lifecycle[]`, which belong
  to the Guard and the Runtime; rejected to keep a Provider's blast radius to
  exactly the capability config and the Assets it might decrypt.

### Option 3: A single global (not per-Alter) decryption identity in `$AXF_HOME`
- Pros: simpler, one file, one `axf keys generate` invocation ever.
- Cons: directly contradicts spec section 3's Alter-scoped identity principle;
  rejected.

## References

- The AXF v0 spec, sections 3, 7, 8, 10, 14, 23 (working document, not checked
  into this repository).
- `runtime/browser.go` (`BrowserProfileProvider`) for the existing per-Alter
  isolated-directory pattern this ADR mirrors.
- [filippo.io/age](https://pkg.go.dev/filippo.io/age)
