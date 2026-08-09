# axf

Reference Go SDK and CLI for **AXF (Alter eXtensible Format)**, an open specification describing an *Alter*: a coherent digital environment representing one operational identity. An Alter gathers everything needed to act coherently and in isolation within a digital environment (browser, shell, SSH, Git, AI accounts, locale, history) so that several independent applications can read, create, modify, exchange and run the same Alter without depending on any single implementation. AXF aims to play the role for a digital identity that OpenAPI plays for an API or OCI for a container: one reference specification, several independent implementations.

> **Status: draft v0 format, v1 runtime. The specification is not stable yet.**
> This repository implements the v0 data-model layer (types, JSON Schema, parser, structural validator, format conformance fixtures) and the v1 activation runtime (`axf up` / `axf down`, the `shell`, `git-identity` and `browser-profile` providers, lifecycle hook execution). The Alter Guard, meaning `policies[]` enforcement and the audit trail, and the Key Event Log are later milestones and are deliberately absent: `axf up` activates a capability whatever `policies[]` says. No cryptography is performed: signature and encryption blocks are carried verbatim, never produced or verified, which is why the `ssh-keypair` and `ai-account` capabilities have no provider yet.

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
runtime/                the v1 runtime: store, Provider contract, providers, up/down
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
