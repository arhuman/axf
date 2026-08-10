# axf

Reference Go SDK and CLI for **AXF (Alter eXtensible Format)**, an open specification describing an *Alter*: a coherent digital environment representing one operational identity. An Alter gathers everything needed to act coherently and in isolation within a digital environment (browser, shell, SSH, Git, AI accounts, locale, history) so that several independent applications can read, create, modify, exchange and run the same Alter without depending on any single implementation. AXF aims to play the role for a digital identity that OpenAPI plays for an API or OCI for a container: one reference specification, several independent implementations.

> **Status: draft v0 format, v1.1 runtime. The specification is not stable yet.**
> This repository implements the v0 data-model layer (types, JSON Schema, parser, structural validator, format conformance fixtures), the v1 activation runtime (`axf up` / `axf down`, the `shell`, `git-identity` and `browser-profile` providers, lifecycle hook execution) and the v1.1 Alter Guard (`policies[]` enforcement on the activation path, audit trail in the shape of spec section 15). The Key Event Log is a later milestone and is deliberately absent. No cryptography is performed: signature and encryption blocks are carried verbatim, never produced or verified, which is why the `ssh-keypair` and `ai-account` capabilities have no provider yet.

## Install and build

```sh
make build      # compile bin/axf (cgo-free)
make test       # go test -race ./...
make cover      # coverage report, gated at 80%
make audit      # mod verify, vet, golangci-lint, govulncheck, coverage gate
make tidy       # go mod tidy + gofmt
make help       # list all targets
```

## CLI

```sh
axf up <name>                # print the shell commands activating an Alter
axf down                     # print the shell commands deactivating the active one
axf audit                    # print the Alter Guard audit trail
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
| `browser-profile` | `{"engine": "firefox\|chrome\|auto", "profileDir": "..."}` | isolates a profile under `$AXF_HOME/profiles/<name>/<engine>` and exports `AXF_BROWSER_PROFILE` |

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

A capability with no provider in this build (`ssh-keypair`, `ai-account`,
`locale`) stops the activation before anything is printed: a partially applied
identity would let you believe a capability is in place when it is not. A
*hook* the runtime cannot honour only warns on stderr and sets a non-zero exit
code, leaving stdout a correct script, and `axf down` always clears
`AXF_ACTIVE_ALTER` even when the document has since been deleted.

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
capability, a provider decides how it is honoured on a given platform.

### Fixtures

`testdata/fixtures/` is the format conformance suite every AXF implementation
should classify identically. One caveat: `invalid/duplicate-asset-name.json`
passes JSON Schema validation on purpose. Its rule is a conformance rule, not a
structural one, so it is the fixture that proves an implementation checks more
than the schema.

## License

MIT. See [LICENSE](LICENSE).
