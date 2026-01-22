# Implementation: Implement IAppTTLStorage interface

## Technical design

- [x] create: [storage/appttl--arch.md](../../specs/prod/storage/appttl--arch.md)
  - Document Application TTL Storage subsystem architecture
  - Define IAppTTLStorage interface placement and relationship with ISysVvmStorage
  - Describe component hierarchy and data flow
- [ ] review

## Construction

- [ ] update: [pkg/istructs/interface.go](../../../pkg/istructs/interface.go)
  - Add `IAppTTLStorage` interface definition with Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete methods
  - Add `AppTTLStorage() IAppTTLStorage` method to `IAppStructs` interface
- [ ] update: [pkg/vvm/storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - Add `pKeyPrefix_AppTTL` constant (value 4)
- [ ] create: [pkg/vvm/storage/impl_appttl.go](../../../pkg/vvm/storage/impl_appttl.go)
  - Implement `implAppTTLStorage` struct wrapping `ISysVvmStorage`
  - Implement `buildKeys()` to construct partition key with `[pKeyPrefix_AppTTL][ClusterAppID]` and clustering columns from user key
  - Implement all `IAppTTLStorage` methods delegating to `ISysVvmStorage`
- [ ] update: [pkg/vvm/storage/provide.go](../../../pkg/vvm/storage/provide.go)
  - Add `NewAppTTLStorage(sysVVMStorage ISysVvmStorage, clusterAppID istructs.ClusterAppID) istructs.IAppTTLStorage` function
- [ ] update: [pkg/istructsmem/provide.go](../../../pkg/istructsmem/provide.go)
  - Add `sysVvmStorage storage.ISysVvmStorage` parameter to `Provide` function
  - Pass `sysVvmStorage` to `appStructsProviderType`
- [ ] update: [pkg/istructsmem/impl.go](../../../pkg/istructsmem/impl.go)
  - Add `sysVvmStorage` field to `appStructsProviderType` struct
  - Add `appTTLStorage` field to `appStructsType` struct
  - Create `IAppTTLStorage` instance in `newAppStructs` using app's ClusterAppID
  - Implement `AppTTLStorage()` method on `appStructsType`
- [ ] update: [pkg/vvm/provide.go](../../../pkg/vvm/provide.go)
  - Update `provideIAppStructsProvider` to accept and pass `ISysVvmStorage` parameter
- [ ] create: [pkg/vvm/storage/impl_appttl_test.go](../../../pkg/vvm/storage/impl_appttl_test.go)
  - Unit tests for `implAppTTLStorage` key building and method delegation
- [ ] Review
