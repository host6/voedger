# Implementation: Implement IAppTTLStorage interface

## Technical design

- [x] create: [storage/appttl--arch.md](../../specs/prod/storage/appttl--arch.md)
  - Document Application TTL Storage subsystem architecture
  - Define IAppTTLStorage interface placement and relationship with ISysVvmStorage
  - Describe component hierarchy and data flow
- [ ] review

## Construction

- [ ] update: [pkg/istructs/interface.go](../../../pkg/istructs/interface.go)
  - Add AppTTLStorage() method to IAppStructs interface returning IAppTTLStorage
- [ ] create: [pkg/istructs/ttlstorage.go](../../../pkg/istructs/ttlstorage.go)
  - Define IAppTTLStorage interface with Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete methods
- [ ] update: [pkg/vvm/storage/interface.go](../../../pkg/vvm/storage/interface.go)
  - Add NewAppTTLStorage function signature or extend ISysVvmStorage if needed
- [ ] create: [pkg/vvm/storage/impl_appttl.go](../../../pkg/vvm/storage/impl_appttl.go)
  - Implement NewAppTTLStorage() that wraps ISysVvmStorage
  - Prepend app-specific prefix to partition key
- [ ] create: [pkg/vvm/storage/impl_appttl_test.go](../../../pkg/vvm/storage/impl_appttl_test.go)
  - Unit tests for AppTTLStorage implementation
- [ ] update: [pkg/istructsmem/impl.go](../../../pkg/istructsmem/impl.go)
  - Add appTTLStorage field to appStructsType
  - Implement AppTTLStorage() method
- [ ] update: [pkg/istructsmem/appstruct-types.go](../../../pkg/istructsmem/appstruct-types.go)
  - Add ttlStorage field to AppConfigType if needed for configuration
- [ ] update: [pkg/vvm/provide.go](../../../pkg/vvm/provide.go)
  - Wire AppTTLStorage into VVM initialization
  - Ensure IAppStructsProvider receives AppTTLStorage dependency
- [ ] Review

