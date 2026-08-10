# Contributing

How to build, test, and submit changes to axf.

## Setup

```sh
git clone https://github.com/arhuman/axf
cd axf
make tools   # install pinned golangci-lint + govulncheck
make build   # compile bin/axf
```

Enable the local commit-message hook once per clone:

```sh
git config core.hooksPath .githooks
```

## Make targets

| Target | Purpose |
| ------ | ------- |
| `make build` | Compile `bin/axf` (cgo-free, version-stamped via `-ldflags`). |
| `make test` | Run all tests with the race detector. |
| `make cover` | Tests with coverage; fails below `COVER_MIN` (80%). |
| `make audit` | `go vet` + `golangci-lint` + `govulncheck` + coverage gate. Same command locally and in CI. |
| `make tidy` | `go mod tidy` + `gofmt`. |
| `make ci` | Full local pipeline (`tidy` + `audit`). Gate for `make release`. |
| `make release` | Derive the next semver from Conventional Commits, gate via `make ci`, stamp `CHANGELOG.md`, tag and push. |
| `make install` | Install `bin/axf` into `GOBIN`. |
| `make clean` | Remove build and coverage artifacts. |

## Test layout

Tests are colocated 1:1 with the source they test (`x.go` alongside
`x_test.go`). Three files are the deliberate exception: `alter.go`,
`runtime/audit.go` and `runtime/script.go` have no same-named test file
because their behavior is exercised through other tests instead
(`schema_test.go`, `extensions_test.go` and `conformance_test.go` for
`alter.go`; `runtime/guard_test.go`, `runtime_test.go` and
`helpers_test.go` for `audit.go` and `script.go`). New source files should
still follow the 1:1 convention; these three predate it.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `type(scope): subject`, type one of
`feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert`. Enforced locally (the
`.githooks/commit-msg` hook, once `core.hooksPath` is set) and in CI (commitlint on PRs).

## Before opening a PR

1. `make ci` passes.
2. Update `CHANGELOG.md` under `[Unreleased]` with a succinct, public-facing entry
   (what changed, not why or how it was decided).
3. New Go code follows the existing doc-comment and error-wrapping conventions; see
   `runtime/provider.go` or `runtime/gitidentity.go` for the pattern.
