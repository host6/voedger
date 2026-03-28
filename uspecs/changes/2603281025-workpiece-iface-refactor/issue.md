# AIR-3446

- Source: <https://untill.atlassian.net/browse/AIR-3446>

## Summary

Refactor workpiece exported functions into common named interfaces in the processors package.

## Details

QPv1, QPv2 and CP workpieces expose exported methods (e.g. `ResetRateLimit`, `AppPartitions`, `AppPartition`, `GetPrincipals`, `Roles`, `Context`) that are never called directly — they are accessed by casting `pipeline.IWorkpiece` to anonymous one-method interfaces at the call site.

### Current anonymous interface casts found

- `pkg/sys/verifier/impl.go` — `args.Workpiece.(interface{ ResetRateLimit(appdef.QName, appdef.OperationKind) })`
- `pkg/sys/authnz/impl_enrichprincipaltoken.go` — `args.Workpiece.(interface{ GetPrincipals() []iauthnz.Principal })`
- `pkg/sys/sqlquery/impl.go` — `args.Workpiece.(interface{ AppPartition() appparts.IAppPartition })`
- `pkg/sys/sqlquery/impl.go` — `args.Workpiece.(interface{ Roles() []appdef.QName })`
- `pkg/sys/sqlquery/impl.go` — `args.Workpiece.(interface{ AppPartitions() appparts.IAppPartitions })`
- `pkg/cluster/impl_vsqlupdate.go` — `args.Workpiece.(interface{ AppPartitions() appparts.IAppPartitions })`
- `pkg/registry/impl_createlogin.go` — `args.Workpiece.(interface{ AppPartitions() appparts.IAppPartitions })`
- `pkg/sys/workspace/impl.go` — `args.Workpiece.(interface{ Context() context.Context })`

### Existing named interface to replace

- `pkg/processors/actualizers/impl.go` defines `syncActualizerWorkpiece` — a local unexported interface embedding `pipeline.IWorkpiece` plus typed methods (`Event()`, `AppPartition()`, `Context()`, `LogCtx()`, `PLogOffset()`)
- This interface is used by sync projector pipeline closures in `syncActualizerFactory` and `newSyncBranch` to access command workpiece data
- `syncActualizerWorkpiece` will be replaced by `ISyncProjectorWorkpiece`, which embeds `ICmdProcWorkpiece` and adds the sync projector-specific methods (`Event`, `AppPartition`, `LogCtx`, `PLogOffset`)

### Goal

- Make `PrepareArgs` generic, define `ICmdProcWorkpiece` and `IQueryProcWorkpiece` interfaces, and parameterize `ExecCommandArgs`/`ExecQueryArgs` with them so that `args.Workpiece` is already typed and anonymous casts are no longer needed
- Replace `syncActualizerWorkpiece` with `ISyncProjectorWorkpiece` (embeds `ICmdProcWorkpiece`) so that the sync actualizer pipeline uses a shared interface hierarchy

