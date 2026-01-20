# Implementation: Implement IAppTTLStorage interface

## Technical design

- [x] create: [storage/appttl--arch.md](../../specs/prod/storage/appttl--arch.md)
  - Document Application TTL Storage subsystem architecture
  - Define IAppTTLStorage interface placement and relationship with ISysVvmStorage
  - Describe component hierarchy and data flow

## Unclear

- // TODO integration tests

## Construction

- [ ] update: [pkg/istructs/interface.go](../../../pkg/istructs/interface.go)
  - Add IAppTTLStorage interface definition with Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete methods
  - Add AppTTLStorage() method to IAppStructs interface
- [ ] update: [pkg/vvm/storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - Add pKeyPrefix_AppTTL constant
- [ ] create: [pkg/vvm/storage/impl_appttl.go](../../../pkg/vvm/storage/impl_appttl.go)
  - Implement implAppTTLStorage struct wrapping ISysVvmStorage
  - Add NewAppTTLStorage() constructor accepting ISysVvmStorage and appdef.AppQName
  - Implement Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete methods
  - Build partition key with pKeyPrefix_AppTTL + AppQName + user pk
- [ ] create: [pkg/vvm/storage/impl_appttl_test.go](../../../pkg/vvm/storage/impl_appttl_test.go)
  - Add unit tests for implAppTTLStorage
- [ ] update: [pkg/vvm/storage/provide.go](../../../pkg/vvm/storage/provide.go)
  - Add NewAppTTLStorage provider function
- [ ] update: [pkg/istructsmem/provide.go](../../../pkg/istructsmem/provide.go)
  - Add ISysVvmStorage parameter to Provide function
  - Store sysVvmStorage in appStructsProviderType
- [ ] update: [pkg/istructsmem/impl.go](../../../pkg/istructsmem/impl.go)
  - Add sysVvmStorage field to appStructsProviderType
  - Add appTTLStorage field to appStructsType
  - Implement AppTTLStorage() method on appStructsType
  - Create AppTTLStorage instance in newAppStructs using sysVvmStorage
- [ ] update: [pkg/vvm/provide.go](../../../pkg/vvm/provide.go)
  - Update provideIAppStructsProvider to pass ISysVvmStorage to istructsmem.Provide
- [ ] update: [pkg/vvm/wire_gen.go](../../../pkg/vvm/wire_gen.go)
  - Regenerate wire bindings with updated provider signature
- [ ] review
