# Changelog

<!-- Succinct and public-facing: what changed, not why or how it was decided. -->

All notable changes to this project are documented here. Format: [Keep a Changelog](https://keepachangelog.com). Entries are grouped under `[Unreleased]` by date, using Added / Changed / Fixed / Removed. Style rules: concise entries, no em dashes, no emojis (see the 10x-documentation skill).

## [Unreleased]

### Added

- AXF v0 data model: JSON Schema (draft 2020-12), Go SDK types, parser and
  validator, format conformance suite, `axf validate`/`schema`/`version`.
- v1 activation runtime: `axf up`/`axf down`, `shell`, `git-identity` and
  `browser-profile` capability providers, lifecycle hook execution.
- v1.1 Alter Guard: `policies[]` enforcement on the activation path, an
  append-only audit trail at `$AXF_HOME/audit.log`, `axf audit`.
- Asset decryption and the `ssh-keypair` provider: `axf keys generate`
  creates a per-Alter age identity; inline Assets decrypt to
  `$AXF_HOME/keys/ssh/<alter>/<asset>` and export `AXF_SSH_KEY_PATH`.
- `docs/ARCHITECTURE.md`: how the runtime, providers and the Alter Guard fit
  together.
