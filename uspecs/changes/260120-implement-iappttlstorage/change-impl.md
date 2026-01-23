# Implementation: Implement IAppTTLStorage interface

## Technical design

- [x] create: [storage/appttl--arch.md](../../specs/prod/storage/appttl--arch.md)
  - Document Application TTL Storage subsystem architecture
  - Define IAppTTLStorage interface placement and relationship with ISysVvmStorage
  - Describe component hierarchy and data flow
  - Define validation rules for key, value, and TTL parameters
- [ ] review

## Construction

- [ ] update: [pkg/istructs/interface.go](../../../pkg/istructs/interface.go)
  - Add `IAppTTLStorage` interface definition with Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete methods
  - Add `AppTTLStorage() IAppTTLStorage` method to `IAppStructs` interface
- [ ] update: [pkg/vvm/storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - Add `pKeyPrefix_AppTTL` constant (value 4)
  - Add validation constants:
    - `MaxKeyLength = 1024` (bytes)
    - `MaxValueLength = 65536` (bytes, 64 KB)
    - `MaxTTLSeconds = 31536000` (365 days)
- [ ] create: [pkg/vvm/storage/errors.go](../../../pkg/vvm/storage/errors.go)
  - Define validation errors:
    - `ErrKeyEmpty` - key is empty string
    - `ErrKeyTooLong` - key exceeds MaxKeyLength bytes
    - `ErrValueTooLong` - value exceeds MaxValueLength bytes
    - `ErrInvalidTTL` - ttlSeconds <= 0 or > MaxTTLSeconds
- [ ] create: [pkg/vvm/storage/impl_appttl.go](../../../pkg/vvm/storage/impl_appttl.go)
  - Implement `implAppTTLStorage` struct wrapping `ISysVvmStorage`
  - Implement `buildKeys()` to construct partition key with `[pKeyPrefix_AppTTL][ClusterAppID]` and clustering columns from user key
  - Implement `validateKey(key string) error` - validates key is non-empty, within length limit, valid UTF-8
  - Implement `validateValue(value string) error` - validates value is within length limit
  - Implement `validateTTL(ttlSeconds int) error` - validates ttlSeconds > 0 and <= MaxTTLSeconds
  - Implement all `IAppTTLStorage` methods with validation before delegating to `ISysVvmStorage`
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
  - Unit tests for validation:
    - Test empty key returns `ErrKeyEmpty`
    - Test key exceeding 1024 bytes returns `ErrKeyTooLong`
    - Test value exceeding 65536 bytes returns `ErrValueTooLong`
    - Test ttlSeconds = 0 returns `ErrInvalidTTL`
    - Test ttlSeconds < 0 returns `ErrInvalidTTL`
    - Test ttlSeconds > 31536000 returns `ErrInvalidTTL`
    - Test valid inputs pass validation and delegate to storage
- [ ] Review
