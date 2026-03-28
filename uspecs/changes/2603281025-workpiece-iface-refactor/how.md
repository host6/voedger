# How: Make PrepareArgs generic, parameterize ExecCmdArgs/ExecQueryArgs with workpiece interfaces

## Approach

- Define `ICmdProcWorkpiece` and `IQueryProcWorkpiece` interfaces in `pkg/processors/interface.go` (or a new file in `pkg/processors`) containing only the methods currently accessed via anonymous interface casts
- Make `PrepareArgs` in `pkg/istructs/recources-types.go` generic: `PrepareArgs[T any]` with `Workpiece T` instead of `Workpiece interface{}`
- Parameterize `CommandPrepareArgs` and `ExecCommandArgs` with `ICmdProcWorkpiece`, and `ExecQueryArgs` with `IQueryProcWorkpiece`
- Replace all 8 anonymous interface casts with direct typed method calls on `args.Workpiece`
- Replace `syncActualizerWorkpiece` in `pkg/processors/actualizers/impl.go` with `ISyncProjectorWorkpiece` — the sync actualizer pipeline (`WireFunc` closures) will use `ISyncProjectorWorkpiece` instead of the local interface, and `syncActualizerWorkpiece` will be deleted
- Ensure `cmdWorkpiece` in `pkg/processors/command/types.go` satisfies `ISyncProjectorWorkpiece` (which embeds `ICmdProcWorkpiece` and adds sync projector methods: `Event`, `AppPartition`, `LogCtx`, `PLogOffset`) and both `queryWork` types in `pkg/processors/query/impl.go` and `pkg/processors/query2/util.go` satisfy `IQueryProcWorkpiece`
- Update `ICommandFunction.Exec` and `IQueryFunction.Exec` signatures in `pkg/istructs/recources-types.go` to accept the parameterized args types
- Update all implementations of `ICommandFunction` and `IQueryFunction` to match new signatures
- Update `ExecQueryCallback` and related closures (`ExecCommandClosure`, `ExecQueryClosure`) if they reference `PrepareArgs` directly

References:

- [pkg/istructs/recources-types.go](../../pkg/istructs/recources-types.go)
- [pkg/processors/command/impl.go](../../pkg/processors/command/impl.go)
- [pkg/processors/command/types.go](../../pkg/processors/command/types.go)
- [pkg/processors/query/impl.go](../../pkg/processors/query/impl.go)
- [pkg/processors/query2/util.go](../../pkg/processors/query2/util.go)
- [pkg/cluster/impl_vsqlupdate.go](../../pkg/cluster/impl_vsqlupdate.go)
- [pkg/registry/impl_createlogin.go](../../pkg/registry/impl_createlogin.go)
- [pkg/sys/verifier/impl.go](../../pkg/sys/verifier/impl.go)
- [pkg/sys/authnz/impl_enrichprincipaltoken.go](../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
- [pkg/sys/sqlquery/impl.go](../../pkg/sys/sqlquery/impl.go)
- [pkg/sys/workspace/impl.go](../../pkg/sys/workspace/impl.go)
- [pkg/processors/actualizers/impl.go](../../pkg/processors/actualizers/impl.go)

