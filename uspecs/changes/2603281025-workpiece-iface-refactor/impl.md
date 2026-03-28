# Implementation plan: Make PrepareArgs generic, parameterize ExecCmdArgs/ExecQueryArgs with workpiece interfaces

## Construction

### Interface definitions

- [x] create: [pkg/processors/types.go](../../pkg/processors/types.go)
  - add: `ICmdProcWorkpiece`, `ISyncProjectorWorkpiece`, `IQueryProcWorkpiece` interfaces

### Core type changes

- [ ] update: [pkg/istructs/recources-types.go](../../pkg/istructs/recources-types.go)
  - update: `PrepareArgs` to `PrepareArgs[T any]` with `Workpiece T`
  - update: `CommandPrepareArgs` to embed `PrepareArgs[ICmdProcWorkpiece]`
  - update: `ExecQueryArgs` to embed `PrepareArgs[IQueryProcWorkpiece]`
  - update: `IQueryFunction.ResultType` signature to `PrepareArgs[IQueryProcWorkpiece]`
  - update: `IState.QueryPrepareArgs()` return type to `PrepareArgs[IQueryProcWorkpiece]`
- [ ] update: [pkg/istructsmem/resources-types.go](../../pkg/istructsmem/resources-types.go)
  - update: `abstractFunction.res` field type to `func(istructs.PrepareArgs[processors.IQueryProcWorkpiece])`
  - update: `abstractFunction.ResultType` signature
  - update: `NewQueryFunctionCustomResult` signature
  - update: `queryFunction.ResultType` signature
  - update: `ExecQueryClosure` type alias
  - update: `ExecCommandClosure` type alias
- [ ] update: [pkg/state/types.go](../../pkg/state/types.go)
  - update: `PrepareArgsFunc` return type to `istructs.PrepareArgs[processors.IQueryProcWorkpiece]`

### State implementation updates

- [ ] update: [pkg/state/stateprovide/impl_host_state.go](../../pkg/state/stateprovide/impl_host_state.go)
  - update: `QueryPrepareArgs()` return type
- [ ] update: [pkg/state/stateprovide/impl_query_processor_state.go](../../pkg/state/stateprovide/impl_query_processor_state.go)
  - update: `QueryPrepareArgs()` return type
- [ ] update: [pkg/coreutils/mock.go](../../pkg/coreutils/mock.go)
  - update: `MockState.QueryPrepareArgs()` return type

### Workpiece construction updates

- [ ] update: [pkg/processors/command/impl.go](../../pkg/processors/command/impl.go)
  - update: `buildCommandArgs` to construct `PrepareArgs[ICmdProcWorkpiece]`
- [ ] update: [pkg/processors/query/impl.go](../../pkg/processors/query/impl.go)
  - update: `newExecQueryArgs` to construct `PrepareArgs[IQueryProcWorkpiece]`
  - update: state factory `PrepareArgsFunc` lambda
- [ ] update: [pkg/processors/query2/impl.go](../../pkg/processors/query2/impl.go)
  - update: `newExecQueryArgs` to construct `PrepareArgs[IQueryProcWorkpiece]`
  - update: state factory `PrepareArgsFunc` lambda

### Sync actualizer replacement

- [ ] update: [pkg/processors/actualizers/impl.go](../../pkg/processors/actualizers/impl.go)
  - remove: `syncActualizerWorkpiece` interface
  - update: `syncActualizerFactory` WireFunc closures to use `processors.ISyncProjectorWorkpiece`
  - update: `newSyncBranch` WireFunc closures to use `processors.ISyncProjectorWorkpiece`

### Anonymous cast replacements

- [ ] update: [pkg/sys/verifier/impl.go](../../pkg/sys/verifier/impl.go)
  - update: Replace `args.Workpiece.(interface{ ResetRateLimit(...) })` with `args.Workpiece.ResetRateLimit(...)`
- [ ] update: [pkg/sys/authnz/impl_enrichprincipaltoken.go](../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
  - update: Replace `args.Workpiece.(interface{ GetPrincipals() ... })` with `args.Workpiece.GetPrincipals()`
- [ ] update: [pkg/sys/sqlquery/impl.go](../../pkg/sys/sqlquery/impl.go)
  - update: Replace `args.Workpiece.(interface{ AppPartition() ... })` with `args.Workpiece.AppPartition()`
  - update: Replace `args.Workpiece.(interface{ Roles() ... })` with `args.Workpiece.Roles()`
  - update: Replace `args.Workpiece.(interface{ AppPartitions() ... })` with `args.Workpiece.AppPartitions()`
- [ ] update: [pkg/cluster/impl_vsqlupdate.go](../../pkg/cluster/impl_vsqlupdate.go)
  - update: Replace `args.Workpiece.(interface{ AppPartitions() ... })` with `args.Workpiece.AppPartitions()`
- [ ] update: [pkg/registry/impl_createlogin.go](../../pkg/registry/impl_createlogin.go)
  - update: Replace `args.Workpiece.(interface{ AppPartitions() ... })` with `args.Workpiece.AppPartitions()`
- [ ] update: [pkg/sys/workspace/impl.go](../../pkg/sys/workspace/impl.go)
  - update: Replace `args.Workpiece.(interface{ Context() ... })` with `args.Workpiece.Context()`

### Other callers

- [ ] update: [pkg/sys/collection/collection_func.go](../../pkg/sys/collection/collection_func.go)
  - update: `collectionResultQName` signature from `istructs.PrepareArgs` to `istructs.PrepareArgs[processors.IQueryProcWorkpiece]`
- [ ] update: [pkg/vit/shared_cfgs.go](../../pkg/vit/shared_cfgs.go)
  - update: `funcWithResponseIntents` lambda parameter type

### Tests

- [ ] update: [pkg/iextengine/wazero/impl_test.go](../../pkg/iextengine/wazero/impl_test.go)
  - update: `cmdPrepareArgsFunc` lambda to construct `PrepareArgs[ICmdProcWorkpiece]`
- [ ] update: [pkg/istructsmem/impl_test.go](../../pkg/istructsmem/impl_test.go)
  - update: `ResultType` call to use `PrepareArgs[IQueryProcWorkpiece]`
- [ ] update: [pkg/state/teststate/impl.go](../../pkg/state/teststate/impl.go)
  - update: `PrepareArgs` construction and `execQueryArgsFunc` lambda
- [ ] update: [pkg/processors/actualizers/impl_helpers_test.go](../../pkg/processors/actualizers/impl_helpers_test.go)
  - update: `cmdWorkpieceMock` to satisfy `ISyncProjectorWorkpiece`
- [ ] Review

