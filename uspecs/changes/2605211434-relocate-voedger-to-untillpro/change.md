---
registered_at: 2026-05-21T14:34:26Z
change_id: 2605211434-relocate-voedger-to-untillpro
type: chore
baseline: 224af22b2f197ae0358add9b4e159b7e60ad5285
issue_url: https://untill.atlassian.net/browse/AIR-4003
---

# Change request: Relocate voedger module to github.com/untillpro/voedger

## Why

The voedger repository is being relocated to `github.com/untillpro/voedger`. The existing import paths `github.com/voedger/voedger/...` must keep working without a sweeping rewrite across the codebase.

## What

The relocation is handled by `replace` directives that redirect `github.com/voedger/voedger` to `github.com/untillpro/voedger`, leaving the `module` line and all import paths untouched.

Scope is limited to modules that are **not** members of `go.work`:

- In-workspace modules (root, `cmd/ctool`, `cmd/edger`, `pkg/iextengine/wazero/_testdata`, `pkg/sys/it/testdata/apps/test2.app1/src`) must **not** carry a `replace` for `github.com/voedger/voedger` — `go.work use .` already supplies the module from local source, and any additional replace produces a `conflicting replacements` error.
- Out-of-workspace modules (`examples/airs-bp2/*`, `cmd/vpm/testdata/**`) get the `replace` so they keep building standalone.
- Downstream consumers add the same `replace` to their own `go.mod`; see the [consumer migration note](#consumer-migration) below.

## Provisioning and configuration

- [x] update: [go.mod](../../../go.mod): drop any `replace` for `github.com/voedger/voedger` (conflicts with `go.work use .`)
  - `go mod edit -dropreplace=github.com/voedger/voedger`
  - `go mod tidy`

- [x] update: [cmd/edger/go.mod](../../../cmd/edger/go.mod): drop the existing `replace github.com/voedger/voedger => ../..` (conflicts with `go.work use .`)
  - `cd cmd/edger && go mod edit -dropreplace=github.com/voedger/voedger && go mod tidy`

- [x] no change: [cmd/ctool/go.mod](../../../cmd/ctool/go.mod), [pkg/iextengine/wazero/\_testdata/go.mod](../../../pkg/iextengine/wazero/_testdata/go.mod), [pkg/sys/it/testdata/apps/test2.app1/src/go.mod](../../../pkg/sys/it/testdata/apps/test2.app1/src/go.mod) — provided by workspace `use`

- [x] update: [examples/airs-bp2/air/go.mod](../../../examples/airs-bp2/air/go.mod): pin to the latest commit on `untillpro/voedger@main` via a `replace` directive
  - `cd examples/airs-bp2/air && go mod edit -replace=github.com/voedger/voedger=github.com/untillpro/voedger@main && go mod tidy`
  - `go mod tidy` rewrites `@main` into the resolved pseudo-version (e.g. `v0.0.0-YYYYMMDDhhmmss-<commit>`)

- [x] update: [examples/airs-bp2/untill/go.mod](../../../examples/airs-bp2/untill/go.mod): add the same `replace` directive
  - `cd examples/airs-bp2/untill && go mod edit -replace=github.com/voedger/voedger=github.com/untillpro/voedger@main && go mod tidy`

- [x] update: [cmd/vpm/testdata/update_voedger_subdirs.sh](../../../cmd/vpm/testdata/update_voedger_subdirs.sh): switch fixture maintenance to the new module path
  - replace `go get github.com/voedger/voedger@main` with `go mod edit -replace=github.com/voedger/voedger=github.com/untillpro/voedger@main && go mod tidy`
  - re-run the script to refresh every `cmd/vpm/testdata/**/go.mod`

- [x] verify: `pkg/iextengine/wazero/_testdata/` tinygo builds — resolved via `go.work use ./pkg/iextengine/wazero/_testdata`; `basicusage/build.sh` succeeds against local voedger source; no `replace` needed

## Consumer migration

Downstream projects depending on `github.com/voedger/voedger` keep their imports and add a `replace` to their own `go.mod` pointing at the latest commit on `untillpro/voedger@main`:

```text
go mod edit -replace=github.com/voedger/voedger=github.com/untillpro/voedger@main
go mod tidy
```

`go mod tidy` resolves `@main` to a pseudo-version (e.g. `v0.0.0-YYYYMMDDhhmmss-<commit>`) and rewrites the `replace` directive accordingly. Re-run the two commands above to bump to a newer `main`.

The `module` line in voedger's relocated `go.mod` remains `github.com/voedger/voedger`, so requiring `github.com/untillpro/voedger` directly (without a `replace`) fails with `module declares its path as: github.com/voedger/voedger, but was required as: github.com/untillpro/voedger`.
