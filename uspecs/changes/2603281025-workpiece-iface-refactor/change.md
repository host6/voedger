---
registered_at: 2026-03-28T10:25:20Z
change_id: 2603281025-workpiece-iface-refactor
baseline: 125e7918dfbcf1539b823560ca2f19743ef85f4e
issue_url: https://untill.atlassian.net/browse/AIR-3446
---

# Change request: Make PrepareArgs generic, parameterize ExecCmdArgs/ExecQueryArgs with workpiece interfaces

## Why

QPv1, QPv2 and CP workpieces expose exported methods (e.g. `ResetRateLimit`, `AppPartitions`, `AppPartition`, `GetPrincipals`, `Roles`, `Context`) that are never called directly — they are accessed by casting `args.Workpiece` (typed as `interface{}`) to anonymous one-method interfaces at the call site. This makes the contract implicit, hard to discover, and fragile when signatures change.

## What

### 1. Make `PrepareArgs` generic

Current:

```go
type PrepareArgs struct {
    Workpiece      interface{}
    ArgumentObject IObject
    WSID           WSID
    Workspace      appdef.IWorkspace
}
```

Proposed:

```go
type PrepareArgs[T any] struct {
    Workpiece      T
    ArgumentObject IObject
    WSID           WSID
    Workspace      appdef.IWorkspace
}
```

### 2. Define `ICmdProcWorkpiece` and `IQueryProcWorkpiece`

Define in the `processors` package (or `istructs`):

Methods are derived from actual anonymous interface casts and from `syncActualizerWorkpiece` (used in sync projectors, currently defined in `pkg/processors/actualizers/impl.go`). Sync projector-specific methods are separated into `ISyncProjectorWorkpiece` which embeds `ICmdProcWorkpiece`.

```go
type ICmdProcWorkpiece interface {
    pipeline.IWorkpiece
    AppPartitions() appparts.IAppPartitions // cluster/impl_vsqlupdate.go, registry/impl_createlogin.go
    Context() context.Context               // sys/workspace/impl.go
}

type ISyncProjectorWorkpiece interface {
    ICmdProcWorkpiece
    AppPartition() appparts.IAppPartition    // syncActualizerWorkpiece (actualizers/impl.go)
    Event() istructs.IPLogEvent              // syncActualizerWorkpiece
    LogCtx() context.Context                 // syncActualizerWorkpiece
    PLogOffset() istructs.Offset             // syncActualizerWorkpiece
}

type IQueryProcWorkpiece interface {
    pipeline.IWorkpiece
    ResetRateLimit(appdef.QName, appdef.OperationKind) // sys/verifier/impl.go
    GetPrincipals() []iauthnz.Principal                // sys/authnz/impl_enrichprincipaltoken.go
    AppPartition() appparts.IAppPartition              // sys/sqlquery/impl.go
    AppPartitions() appparts.IAppPartitions             // sys/sqlquery/impl.go
    Roles() []appdef.QName                             // sys/sqlquery/impl.go
}
```

### 3. Parameterize `ExecCommandArgs` and `ExecQueryArgs`

```go
type CommandPrepareArgs struct {
    PrepareArgs[ICmdProcWorkpiece]
    ArgumentUnloggedObject IObject
}

type ExecCommandArgs struct {
    CommandPrepareArgs
    State   IState
    Intents IIntents
}

type ExecQueryArgs struct {
    PrepareArgs[IQueryProcWorkpiece]
    State   IState
    Intents IIntents
}
```

### 4. Replace anonymous interface casts

All anonymous interface assertions across the following files will be replaced with direct method calls on the typed `Workpiece`:

- `pkg/sys/verifier/impl.go` — `args.Workpiece.ResetRateLimit(...)` instead of `args.Workpiece.(interface{ ResetRateLimit(...) })`
- `pkg/sys/authnz/impl_enrichprincipaltoken.go` — `args.Workpiece.GetPrincipals()`
- `pkg/sys/sqlquery/impl.go` — `args.Workpiece.AppPartition()`, `args.Workpiece.Roles()`, `args.Workpiece.AppPartitions()`
- `pkg/cluster/impl_vsqlupdate.go` — `args.Workpiece.AppPartitions()`
- `pkg/registry/impl_createlogin.go` — `args.Workpiece.AppPartitions()`
- `pkg/sys/workspace/impl.go` — `args.Workpiece.Context()`

### 5. Replace `syncActualizerWorkpiece` with `ISyncProjectorWorkpiece`

`pkg/processors/actualizers/impl.go` defines a local `syncActualizerWorkpiece` interface used by sync projector pipeline closures. `ISyncProjectorWorkpiece` embeds `ICmdProcWorkpiece` and adds the sync projector-specific methods (`Event`, `AppPartition`, `LogCtx`, `PLogOffset`):

- Delete `syncActualizerWorkpiece` from `pkg/processors/actualizers/impl.go`
- Replace all `syncActualizerWorkpiece` type references in `syncActualizerFactory` and `newSyncBranch` with `ISyncProjectorWorkpiece`

### 6. Ensure workpiece types implement the interfaces

- `cmdWorkpiece` (CP) implements `ISyncProjectorWorkpiece` (and therefore `ICmdProcWorkpiece`)
- `queryWork` (QPv1, QPv2) implements `IQueryProcWorkpiece`

See [issue.md](issue.md) for details.