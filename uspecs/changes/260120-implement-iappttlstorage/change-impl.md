# Implementation: Implement IAppTTLStorage interface

## Principles

- Use SysView_TTL (24) as low-level system view backed by IAppStorage
- Persistent storage using existing storage backends (Cassandra, BoltDB, etc.)
- No separate in-memory implementation - reuse existing storage infrastructure

## Technical design

- [ ] update: [storage/structs--arch.md](../../specs/prod/storage/structs--arch.md)
  - Add SysView_TTL (24) to system views table
  - Document SysView_TTL PK/CC layout: PK=[uint16:24][string:Bucket], CC=[string:Key], Value=[bytes:Value][int64:ExpireAt]
  - Add IAppTTLStorage to component hierarchy diagram

## Instantiation flow

```text
VVM Startup
    |
    v
IAppStorageProvider (Cassandra/BoltDB/mem)
    |
    v
IAppStorageProvider.AppStorage(appQName)
    |
    v
IAppStorage (per-app storage)
    |
    +---> ttl.New(IAppStorage, ITime)
    |         |
    |         v
    |     IAppTTLStorage
    |         |
    v         v
newAppStructs(appCfg, buckets, appTokens, appTTLStorage, ...)
    |
    v
appStructsType
    |
    +---> AppTTLStorage() IAppTTLStorage
    |
    v
Application code (device authorization endpoints)
```

## Construction

- [ ] update: [pkg/istructsmem/internal/consts/qnames.go](../../../pkg/istructsmem/internal/consts/qnames.go)
  - Add SysView_TTL constant (24)
- [ ] update: [pkg/istructsmem/internal/vers/const.go](../../../pkg/istructsmem/internal/vers/const.go)
  - Add SysTTLVersion version key
- [ ] update: [pkg/istructs/interface.go](../../../pkg/istructs/interface.go)
  - Add IAppTTLStorage interface with Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete
  - Add AppTTLStorage() method to IAppStructs interface
- [ ] create: [pkg/istructsmem/internal/ttl/impl.go](../../../pkg/istructsmem/internal/ttl/impl.go)
  - Implement TTL storage using SysView_TTL and IAppStorage
  - Key building: pKey=[SysView_TTL][Bucket], cCols=[Key]
  - Value format: [bytes:Value][int64:ExpireAt]
- [ ] create: [pkg/istructsmem/internal/ttl/provide.go](../../../pkg/istructsmem/internal/ttl/provide.go)
  - Factory function New(storage IAppStorage, iTime ITime) IAppTTLStorage
- [ ] create: [pkg/istructsmem/internal/ttl/impl_test.go](../../../pkg/istructsmem/internal/ttl/impl_test.go)
  - Unit tests for Get, InsertIfNotExists, CompareAndSwap, CompareAndDelete
  - Tests for TTL expiration behavior
  - Tests for concurrent access
- [ ] update: [pkg/istructsmem/impl.go](../../../pkg/istructsmem/impl.go)
  - Add appTTLStorage field to appStructsType
  - Implement AppTTLStorage() method on appStructsType
- [ ] update: [pkg/istructsmem/provide.go](../../../pkg/istructsmem/provide.go)
  - Create TTL storage using internal/ttl.New()
  - Wire to appStructsType
- [ ] Review
