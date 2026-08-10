# axf

Managing multiple digital identities is messy: a day job, a freelance client, a side project, a pseudonymous account. Switching between them usually means juggling `~/.ssh/config` rules, overriding `.gitconfig` so you don't accidentally commit with the wrong email, and running separate browser windows so cookies and history don't bleed together.

`axf` fixes that. It lets you switch between isolated digital identities in a single shell session, on demand, with no daemon. An identity here means the things you routinely trip over when you juggle contexts: git author and committer, browser profile, SSH key selection, and a few runtime policies. You activate one identity with `axf up <name>` and deactivate it with `axf down`. The runtime prints shell `export` and `unset` commands, so the calling shell stays in control.

> Status: draft v0 format, v1.1 runtime. The specification is not stable yet.

## 1. The pain and the goal (why)

If you regularly switch between contexts, you have probably done some version of this:

- committing with the wrong git identity
- opening the wrong browser profile and mixing accounts, cookies, and history
- using the wrong SSH key for a repo or a server
- hacking around it with multiple terminals, multiple user accounts, or a pile of ad hoc shell scripts

The goal of `axf` is to make identity switching explicit, repeatable, and easy to audit:

- one command to enter an identity context for the current shell
- one command to exit it cleanly
- identities stored as plain JSON documents you can version and review
- no background process, no hidden state

## 2. Real life examples (how)

### Example A: consultant switching between two clients

You work with Client A and Client B. Each needs its own git identity, its own browser profile, and sometimes a dedicated SSH key.

1. Put your Alter documents in the default store:

- `~/.axf/alters/client-a.json`
- `~/.axf/alters/client-b.json`

2. Activate Client A in your current shell:

```sh
eval "$(axf up client-a)"
```

At this point, the runtime has only printed environment changes. It does not start a daemon and it does not need a state file.

3. Your tools now see the identity via environment variables:

- git sees `GIT_AUTHOR_*` and `GIT_COMMITTER_*`
- your shell prompt can use `AXF_PROMPT_LABEL`
- a browser launch hook can use the per Alter browser profile directory
- SSH key selection can be wired from `AXF_SSH_KEY_PATH`

4. Switch to Client B in the same terminal:

```sh
eval "$(axf up client-b)"
```

`axf up` will tear down the previously active Alter first (based on `AXF_ACTIVE_ALTER`), then activate the new one.

5. Leave the identity context:

```sh
eval "$(axf down)"
```

### Example B: a minimal Alter you can copy and adapt

This is the shape of an Alter document that is immediately useful with this runtime. The store name is the file stem passed to `axf up <name>`, and it does not need to equal `metadata.name`.

```json
{
  "apiVersion": "axf/v0",
  "kind": "Alter",
  "metadata": {
    "id": "urn:axf:alter:00000000-0000-0000-0000-000000000000",
    "name": "client-a",
    "owner": {
      "keyType": "ed25519",
      "publicKeyFingerprint": "sha256:placeholder"
    }
  },
  "capabilities": [
    { "type": "shell", "config": { "promptLabel": "client-a" } },
    { "type": "git-identity", "config": { "name": "Client A", "email": "dev@client-a.example" } },
    { "type": "browser-profile", "config": { "engine": "firefox", "profileDir": "client-a/firefox" } },
    { "type": "ssh-keypair", "config": { "asset": "ssh-key" } }
  ],
  "assets": [
    {
      "name": "ssh-key",
      "kind": "ref",
      "uri": "/home/you/.ssh/client_a_ed25519",
      "ownerSignature": {
        "algorithm": "ed25519",
        "canonicalization": "JCS-RFC8785",
        "signedAt": "2026-08-09T10:00:00Z",
        "value": "base64:placeholder"
      }
    }
  ],
  "lifecycle": {
    "hooks": {
      "postActivation": [{ "capability": "browser-profile", "action": "launch" }]
    }
  }
}
```

A few practical notes:

- If you prefer not to store key material paths directly, use an `inline` Asset instead and let `ssh-keypair` decrypt it at activation time. See the SSH section below.
- `AXF_SSH_KEY_PATH` is informational. To actually use it, wire it into your shell, for example:

```sh
export GIT_SSH_COMMAND="ssh -i $AXF_SSH_KEY_PATH -o IdentitiesOnly=yes"
```

## 3. Install

This repository currently documents building from source.

```sh
make build      # compile bin/axf (cgo-free)
make test       # go test -race ./...
make cover      # coverage report, gated at 80%
make audit      # mod verify, vet, golangci-lint, govulncheck, coverage gate
make tidy       # go mod tidy + gofmt
make help       # list all targets
```

After `make build`, the binary is at `bin/axf`. Put it on your `PATH` or invoke it directly.

Want to contribute? See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, the
make targets, and the commit-message convention.

## 4. Architecture and what AXF is (what)

AXF (Alter eXtensible Format) is an open JSON specification for describing an Alter, a coherent operational identity. The format is meant to be shared across independent tools, so that multiple applications can read, create, validate, and run the same identity document. AXF aims to play the role for a digital identity that OpenAPI plays for an API, or OCI for a container: one reference specification, several independent implementations. See [docs/SPEC.md](docs/SPEC.md) for the full specification this repository implements.

This repository provides:

- a reference Go SDK for the v0 data model (types, JSON Schema, parser, structural validator, conformance fixtures)
- a local activation runtime exposed as a CLI (`axf up` and `axf down`)
- providers for `shell`, `git-identity`, `browser-profile`, and `ssh-keypair`
- the Alter Guard (policy enforcement on the activation path, plus an audit trail)

It deliberately does not implement later milestones:

- the Key Event Log is absent
- signature blocks are carried verbatim, never produced or verified
- cryptography is limited to reading for `ssh-keypair` when decrypting an inline Asset with a local age identity
- this build has no CLI command producing ciphertext for inline Assets
- the `ai-account` capability has no provider

For how the runtime, providers and capabilities fit together internally
(no daemon, one process per invocation, the Provider contract, the Alter
Guard's decision model), see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## CLI

```sh
axf up <name>                # print the shell commands activating an Alter
axf down                     # print the shell commands deactivating the active one
axf audit                    # print the Alter Guard audit trail
axf keys generate <name>     # create the decryption identity of an Alter, print its recipient
axf validate alter.json      # schema + conformance validation, one or more files
axf schema                   # print the embedded JSON Schema
axf version
```

`validate` prints warnings that do not fail the document, such as an inline
asset held by an Alter with no recovery key configured.

## Activating an Alter

`up` and `down` write shell commands to stdout for the calling shell to
evaluate. There is no daemon and no state file:

```sh
eval "$(axf up systems-alchemist)"
eval "$(axf down)"
```

Alters are read from `$AXF_HOME/alters/<name>.json`, `$AXF_HOME` defaulting to
`~/.axf`. `<name>` is the file name stem and need not equal `metadata.name`.
Which Alter is active is carried by `AXF_ACTIVE_ALTER` in the shell that
evaluated `axf up`, per spec section 12: activation is a runtime fact, never a
field of the document. `axf up` reads that variable to know what to tear down
first when switching Alters; `axf down` reads it to know what to undo. Running
`axf down` with nothing active is a no-op, not an error.

| Capability | Config | Effect |
|---|---|---|
| `shell` | `{"promptLabel": "alchemist"}` | exports `AXF_ALTER_NAME` and `AXF_PROMPT_LABEL` for your own `PS1` |
| `git-identity` | `{"name": "...", "email": "..."}` | exports `GIT_AUTHOR_*` and `GIT_COMMITTER_*`, overriding `.gitconfig` without touching it |
| `browser-profile` | `{"engine": "firefox\|chrome\|auto", "profileDir": "..."}` | isolates a profile under `$AXF_HOME/profiles/<name>/<engine>` (or `profileDir`, which must still resolve inside `$AXF_HOME/profiles`) and exports `AXF_BROWSER_PROFILE` |
| `ssh-keypair` | `{"asset": "<assets[].name>"}` | resolves that Asset to a private key path and exports `AXF_SSH_KEY_PATH` |

Every capability is expressed as environment variables so that deactivation is a
plain `unset` leaving nothing behind on disk. `browser-profile` starts no
browser on activation; launching is the `launch` action, so an Alter opts in
with a lifecycle hook and re-activating the same Alter in a second terminal does
not open a second window:

```json
"lifecycle": { "hooks": {
  "postActivation": [{ "capability": "browser-profile", "action": "launch" }]
} }
```

A capability with no provider in this build (`ai-account`, `locale`) stops the
activation before anything is printed: a partially applied
identity would let you believe a capability is in place when it is not. A
*hook* the runtime cannot honour only warns on stderr and sets a non-zero exit
code, leaving stdout a correct script, and `axf down` always clears
`AXF_ACTIVE_ALTER` even when the document has since been deleted.

## SSH keys and inline Assets

`ssh-keypair` names one `assets[]` entry and resolves it to a private key path.
A `ref` Asset is a key you already manage: its `uri` is checked to be a regular
file and exported as is, nothing is copied or decrypted. An `inline` Asset is
decrypted and written to `$AXF_HOME/keys/ssh/<name>/<asset>`, mode `0600` under
a `0700` directory, the same per-Alter isolation browser profiles get.

Decryption needs a local age identity, one per Alter, so that no single key can
correlate two of them:

```sh
axf keys generate systems-alchemist    # prints an age1... recipient
```

The identity is written to `$AXF_HOME/keys/<name>.age` (mode `0600`) and its
recipient printed on stdout. Add that line to
`assets[].encryption.recipients[]` yourself; nothing edits a document for you,
and this build has no command producing a ciphertext either. Creating one
today means writing Go against `filippo.io/age` directly and pasting the
result into an Alter document by hand; there is no CLI command for it yet.
Generating twice for the same Alter is refused rather than silently replacing
the key every existing ciphertext was encrypted for: rotating means removing
the file first, knowingly.

`AXF_SSH_KEY_PATH` is informational, like `AXF_BROWSER_PROFILE`: no tool reads
it on its own. Wiring it into `ssh` or `git` is your shell's business:

```sh
export GIT_SSH_COMMAND="ssh -i $AXF_SSH_KEY_PATH -o IdentitiesOnly=yes"
```

## The Alter Guard

The Guard is the runtime component that applies `policies[]` and produces the
audit trail (spec sections 4, 11 and 15). It needs no subcommand: it runs inside
`axf up` and `axf down`.

For one `(capability, action)` pair, every `policies[]` entry matching both is
evaluated. **Deny overrides**: one matching `deny` is enough, and no `allow` can
clear it. AXF v0 has no default-deny baseline, so an `allow` entry decides
nothing by itself; it exists to put an explicit line in the trail. An action no
policy targets proceeds.

`scope` decides blocking, not auditing. `runtime` stops the action before it
runs; `guard`, the schema default, records the same denial and lets it proceed.
Exactly one audit event is written per evaluation either way, including when no
policy matched at all.

Activating a capability is matched as `action: "activate"` and deactivating it as
`action: "deactivate"`. Neither name appears in the schema, which only ever names
the actions carried by a lifecycle hook: they are a convention of this runtime.

```json
"policies": [
  {
    "capability": "browser-profile",
    "action": "launch",
    "effect": "deny",
    "scope": "runtime",
    "condition": { "type": "hostname", "operator": "in", "value": ["work-laptop"] }
  }
]
```

A denied capability or hook stops `axf up` before a single line reaches stdout,
the same fail-closed posture a capability with no provider has. Deactivation is
audited but **never** blocked: a deny policy must not be able to strand a shell
inside an Alter, so `axf down` evaluates no condition at all.

Conditions read `runtime.GOOS` (`os`), `os.Hostname()` (`hostname`),
`AXF_ACTIVE_ALTER` (`activeAlterName`) and an arbitrary environment variable
named by `condition.name` (`env`). During `axf up <name>` the `activeAlterName`
fact still holds the *previous* Alter, since the new value only reaches the
shell through the `export` line it has not evaluated yet. A condition that
cannot be evaluated fails closed and aborts the activation rather than being
read as false; an `env` condition with no `name` set is one such case.

`activeAlterName` compares the store name a runtime activated the Alter under
(the `<name>` in `axf up <name>`), not `metadata.id`: the two are distinct by
design (spec §7.1), and a URN would be impractical to type or read in a shell.

The trail is appended to `$AXF_HOME/audit.log`, one JSON event per line:

```sh
axf audit | jq 'select(.result == "denied")'
```

`actor` is a random token generated once per invocation and never persisted,
never the real PID: spec section 15 forbids leaking host process identifiers,
which would themselves de-pseudonymise the Alter. Events carry capability, action
and provider names only, so no decrypted secret can reach the log.

Enforcement covers the actions axf performs and nothing else. A browser started
outside axf is not policed; that would need OS-level integration a CLI with no
daemon cannot have.

## SDK

```go
import (
    alter "github.com/arhuman/axf"
    "github.com/arhuman/axf/conformance"
)

doc, err := alter.Parse(data)          // schema validation, then decode
if err != nil { /* structurally invalid */ }

if err := conformance.Check(doc); err != nil { /* rule violation */ }

for _, w := range conformance.Warnings(doc) {
    log.Println(w)
}
```

`alter.Validate` runs the embedded JSON Schema against raw bytes and nothing
else. Cross-field rules the schema cannot express, asset name uniqueness above
all, live in `conformance` so that the schema stays the artifact other language
SDKs can share unchanged.

Unknown `x-<vendor>` members are preserved at every object level and re-emitted
on marshal, so a tool that does not understand an extension never drops it.
Timestamps are kept as strings rather than `time.Time` because the content
digest is computed over canonical bytes and reformatting an equivalent
timestamp would change it.

## Layout

```
.                       package alter: the v0 types, Parse, Validate
conformance/            rules JSON Schema cannot express, plus SDK warnings
runtime/                the runtime: store, Provider contract, providers, up/down, Alter Guard
schema/alter.schema.json  canonical JSON Schema (draft 2020-12), embedded via go:embed
cmd/axf/                CLI entry point
internal/cli/           CLI implementation
internal/version/       build metadata injected at link time
testdata/fixtures/      conformance fixtures, valid/ and invalid/
testdata/alters/        Alter documents the runtime tests activate
```

The importable API is the root `alter` package, `conformance` and `runtime`.
Everything under `internal/` is CLI wiring and carries no compatibility promise.
`runtime.Provider` is the extension point of spec section 3: the format names a
capability, a provider decides how it is honoured on a given platform. See
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how the pieces under
`runtime/` fit together.

### Fixtures

`testdata/fixtures/` is the format conformance suite every AXF implementation
should classify identically. One caveat: `invalid/duplicate-asset-name.json`
passes JSON Schema validation on purpose. Its rule is a conformance rule, not a
structural one, so it is the fixture that proves an implementation checks more
than the schema.

## License

MIT. See [LICENSE](LICENSE).
