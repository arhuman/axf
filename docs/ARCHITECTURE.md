# Architecture

This document describes how this repository implements AXF: the Go types,
the runtime, and how a `capabilities[]` entry in a document becomes an
exported environment variable in your shell. It is implementation, not
specification. The AXF format itself (what an Alter *is*) is defined by the
spec this repository tracks and by `schema/alter.schema.json`; nothing here
constrains a different AXF implementation, which is free to be a daemon, a
web service, or anything else the spec's "Provider-based" principle allows.

## The one fact that explains the rest

**There is no daemon.** Every `axf` invocation is a short-lived process: it
starts, does its work, prints a shell script to stdout, and exits. Nothing
listens on a socket, nothing runs in the background, nothing survives
between one `axf up` and the next `axf down`. The only things that outlive
the process are files under `$AXF_HOME` and the environment variables the
*calling shell* set by evaluating what `axf` printed. That single constraint
shapes almost every other decision described below: providers are compiled
into the binary rather than loaded as plugins, the audit log is opened,
appended to and closed on every single write, and `EnvVarNames` has to be a
pure function because `axf down` cannot ask a long-gone `axf up` what it did.

## The pieces

```
alter.Alter (the document)         runtime.Runtime (one invocation)
├── Capabilities[]  ───────┐       ├── Store            loads + validates a document
├── Assets[]          ↕    │       ├── Registry          capability name -> Provider
├── Policies[]         (used by)   ├── Guard (per doc)   policies[] + audit trail
└── Lifecycle.Hooks        │       └── script            builds the export/unset text
                            └──> runtime.Provider (one per capability)
                                 ├── Target   {Name, Home, Assets}
                                 ├── Config   capabilities[].config, verbatim
                                 └── Activate / EnvVarNames / Run
```

- **`alter.Alter`** (`alter.go`) is the parsed, schema-validated document:
  plain data, no behavior. `runtime` never mutates it.
- **`runtime.Store`** (`runtime/store.go`) resolves a store name to a file
  under `$AXF_HOME/alters/<name>.json`, reads it, and runs it through
  `alter.Parse` (schema) and `conformance.Check` (cross-field rules the
  schema cannot express, such as asset name uniqueness). A document failing
  either is never activated.
- **`runtime.Provider`** (`runtime/provider.go`) is the interface one
  capability implementation satisfies: `shell`, `git-identity`,
  `browser-profile`, `ssh-keypair` each have one, in their own file
  (`runtime/shell.go`, `gitidentity.go`, `browser.go`, `sshkeypair.go`).
- **`runtime.Registry`** is a `map[string]Provider` built fresh by
  `DefaultRegistry()` on every invocation, not persisted or cached anywhere.
- **`runtime.Guard`** (`runtime/guard.go`) is built per document, applies
  `policies[]`, and writes to the audit log. See "The Alter Guard" below.
- **`runtime.Runtime`** (`runtime/runtime.go`) is the orchestrator: one
  value per invocation, holding a `Store`, a `Registry`, and which Alter the
  calling shell reported as active.

## The Provider contract

```go
type Provider interface {
    Capability() string
    Activate(t Target, cfg Config) (ActivationResult, error)
    EnvVarNames(t Target, cfg Config) []string
    Run(t Target, action string, cfg Config) error
}
```

- **`Target`** is what every call gets: the Alter's store name, `$AXF_HOME`,
  and (since the `ssh-keypair` provider needed it) the document's
  `Assets[]`. It carries no `policies[]` or `lifecycle[]`: a Provider's
  reach stops at the capability config and the Assets it might decrypt,
  those other two belong to the Guard and the Runtime respectively.
- **`Config`** is `capabilities[].config` verbatim, with a `String(key)`
  helper that treats an absent key as `""` and a present-but-wrong-type key
  as an error (an omission and a mistake are different things).
- **`Activate`** does the work and returns the environment to export. It
  must be idempotent: re-activating an already-active Alter (running `axf up`
  twice, or in a second terminal) runs it again and must not fail or
  double up.
- **`EnvVarNames`** must be pure: no host access, no error, just "what
  would `Activate` export for this config." This is what lets `axf down`
  unset exactly the right variables without ever having seen the
  corresponding `axf up`, and without touching the network, the filesystem,
  or a secret. It is the one method both `Up` and `teardown` call.
- **`Run`** executes a lifecycle hook action that isn't plain activation
  (`{"capability": "browser-profile", "action": "launch"}`). Most providers
  implement none and return an error wrapping `ErrActionNotImplemented`.

Adding a fifth provider means writing a Go file that implements this
interface and adding one line to `DefaultRegistry()`, then rebuilding the
binary. There is no dynamic loading: a capability with no provider in a
given build (today, `ai-account` and `locale`) is a compile-time fact, not
a missing plugin you could install.

## One request, start to finish: `axf up client-a`

1. `internal/cli.Run` dispatches to `runUp`, which calls `newRuntime()`:
   reads `$AXF_HOME` and `$AXF_ACTIVE_ALTER` from the process environment
   and builds a `Runtime` with a fresh `DefaultRegistry`.
2. `Runtime.Up("client-a", stdout)`:
   - `Store.Load` reads, schema-validates and conformance-checks the
     document. Any failure stops here, nothing is printed.
   - `resolveAll` looks up every declared capability in the `Registry` up
     front, collecting every missing one before failing, so a document
     naming two unimplemented capabilities reports both in one pass rather
     than one-at-a-time.
   - `authorize` asks the `Guard` about every action the activation is
     about to take, in order (`preActivation` hooks, then each capability's
     `activate`, then `postActivation` hooks), before anything is torn
     down, activated or printed. A denial here aborts with empty stdout,
     the same fail-closed posture as a missing provider.
   - If a different Alter is currently active, its teardown is appended to
     the script first.
   - `preActivation` hooks run (best-effort: a hook whose provider or
     action this build cannot honor is reported on stderr, never fatal).
   - Each capability's `Provider.Activate` runs, in document order, and its
     `Env` map is written into the script as sorted `export NAME='value'`
     lines (`runtime/script.go`).
   - `postActivation` hooks run, then `AXF_ACTIVE_ALTER` is exported.
   - The accumulated script is written to stdout in one call: nothing
     reaches stdout until the whole activation is known to be valid.
3. The calling shell's `eval "$(axf up client-a)"` evaluates those lines.
   `axf` itself has already exited.

`axf down` is the mirror image: `teardown` loads the previously active
document (tolerating its absence), calls `Guard.Record` (never `Check`,
deactivation cannot be blocked) and each provider's pure `EnvVarNames`, and
emits `unset` lines. `AXF_ACTIVE_ALTER` is always unset, even when the
document has since been deleted or a provider is gone from this build.

## The Alter Guard

`runtime.Guard` (`runtime/guard.go`) applies `policies[]` to one
`(capability, action)` pair at a time. The decision model, in full:

- Every policy whose `capability` and `action` both match is evaluated.
- An entry with no `condition` always matches; one whose condition doesn't
  hold is skipped.
- **Deny overrides**: a single matching `deny` is enough, and no `allow` can
  clear it. There is no default-deny baseline in AXF v0, so an `allow`
  entry never decides anything by itself, it only puts an explicit line in
  the audit trail. An action no policy targets proceeds.
- `scope: runtime` blocks the action before it runs. `scope: guard` (the
  schema default) records the identical denial and lets the action proceed
  anyway. Exactly one audit event is written per evaluation, including when
  no policy matched at all.
- A condition the Guard cannot evaluate (an `env` condition with no `name`,
  an unknown `type` or `operator`, a malformed `value`) **fails closed**:
  the activation aborts rather than treating the unknown condition as
  false.
- `Check` (activation path) can block. `Record` (deactivation path)
  evaluates no condition at all and is always `allowed`: a deny policy must
  never be able to strand a shell inside an Alter.

The audit trail (`runtime/audit.go`) is JSON Lines, appended to
`$AXF_HOME/audit.log` with `O_APPEND` on every write, opened and closed each
time, since there is no daemon to hold it open. Each event carries
`capability`, `action` and `provider` names only, never a value read from an
Asset: `redactSecrets` is structurally satisfied because there is no field a
decrypted secret could ever reach. `actor` is a random per-invocation token
(`NewActor`, `crypto/rand`), never the host's real PID, so two runs can't be
correlated through the log and the log can't itself de-pseudonymize the
Alter.

## Shell script generation is a trust boundary

`axf up`'s stdout is meant to be `eval`'d by the calling shell, which makes
it the one place a bug would be a command injection, not just a bad output.
`runtime/script.go` treats that boundary explicitly:

- `export`/`unset` reject any variable name that isn't a POSIX-portable
  identifier (`^[A-Za-z_][A-Za-z0-9_]*$`) before writing it, so a provider
  can never smuggle a command through a variable *name*.
- Every value is wrapped in POSIX single quotes (`quote`), the one shell
  quoting form with no escape sequences: every byte but `'` is literal, and
  `'` itself is closed, escaped, and reopened. A value holding spaces,
  quotes, or shell metacharacters survives `eval` as inert data.

## The capability registry is not the provider registry

`alter.CapabilityRegistry` (`alter.go`) is the format's list of canonical
capability *names* (`shell`, `browser-profile`, `git-identity`,
`ssh-keypair`, `locale`, `ai-account`): what an AXF document is allowed to
call itself without a vendor prefix. `runtime.DefaultRegistry()` is this
build's list of *implementations*. The two are not the same list on
purpose, and a name being in the first without being in the second is not a
bug: `locale` has never had a defined config shape or effect (nobody has
written the ADR deciding what it should do), and `ai-account` needs the
same asset-decryption plumbing `ssh-keypair` uses but a different output
shape (a credential more likely belongs in a tool-specific config than in a
file path). `missingReason()` in `provider.go` is what turns "no provider"
into an explanation rather than a bare lookup failure.

## What this document does not cover

- The AXF format itself: the normative spec is [docs/SPEC.md](SPEC.md); see
  also the root `alter` package doc comment and `README.md`'s SDK section
  for how this repository's types, JSON Schema and conformance rules
  implement it.
- Feature-specific design decisions: see `docs/adr/`. ADR 0001 is a worked
  example of one (asset decryption and the `ssh-keypair` provider),
  including alternatives considered and rejected.
- What's deliberately unbuilt: the Key Event Log, signature verification, a
  provider conformance suite, Policy targeting by Asset. See the status
  paragraph near the top of `README.md`.
