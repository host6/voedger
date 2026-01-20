# Context subsystem architecture: Application TTL storage

## Overview

Application TTL Storage provides a workspace-agnostic, in-memory key-value storage with automatic expiration (TTL) capabilities. It enables applications to store temporary data that automatically expires after a specified duration, with atomic operations for race-condition-free updates.

Primary use cases:

- Device linking flow (Alpha Code to Device Code mappings)
- Temporary token storage with automatic expiration
- Any application-level temporary state requiring TTL semantics

## Architecture

### Component hierarchy

```text
Application Layer (air.ACDeviceAuthorizationRequest, etc.)
    |
    v
IAppStructs.AppTTLStorage() (pkg/istructs)
    |
    +-- IAppTTLStorage interface (string-based keys)
    |
    v
AppTTLStorage Implementation (pkg/vvm/storage)
    |
    +-- Prepends app-specific prefix to partition key
    |
    v
ISysVvmStorage (pkg/vvm/storage)
    |
    +-- Low-level byte-based TTL operations
    |
    v
IAppStorage (pkg/istorage)
    |
    v
Storage Backend (in-memory with TTL support)
```

### Interface definitions

IAppTTLStorage (pkg/istructs):

```go
type IAppTTLStorage interface {
    // Get retrieves value by partition key and clustering column
    Get(pk, cc string) (value string, exists bool)
    // InsertIfNotExists inserts only if key doesn't exist
    InsertIfNotExists(pk, cc, value string, ttlSeconds int) bool
    // CompareAndSwap performs atomic update with TTL reset
    CompareAndSwap(pk, cc, expectedValue, newValue string, ttlSeconds int) bool
    // CompareAndDelete performs atomic deletion with value verification
    CompareAndDelete(pk, cc, expectedValue string) bool
}
```

ISysVvmStorage (pkg/vvm/storage) - existing low-level interface:

```go
type ISysVvmStorage interface {
    InsertIfNotExists(pKey []byte, cCols []byte, value []byte, ttlSeconds int) (ok bool, err error)
    CompareAndSwap(pKey []byte, cCols []byte, oldValue, newValue []byte, ttlSeconds int) (ok bool, err error)
    CompareAndDelete(pKey []byte, cCols []byte, expectedValue []byte) (ok bool, err error)
    Get(pKey []byte, cCols []byte, data *[]byte) (ok bool, err error)
    // ... other methods
}
```

### Key design decisions

Application isolation:

- Each application gets isolated storage via app-specific prefix in partition key
- Prefix format: `[pKeyPrefix_AppTTL][AppQName]` prepended to user-provided pk
- Prevents cross-application data access

String-based interface:

- IAppTTLStorage uses string keys/values for simplicity at application level
- Implementation converts to bytes for ISysVvmStorage

No error returns:

- IAppTTLStorage methods return bool only (no error)
- Errors are logged internally, failures return false
- Simplifies application-level code

### Data flow

Write operation (InsertIfNotExists):

```text
1. Application calls AppTTLStorage().InsertIfNotExists(pk, cc, value, ttl)
2. AppTTLStorage prepends app prefix to pk
3. Converts strings to bytes
4. Calls ISysVvmStorage.InsertIfNotExists(prefixedPK, cc, value, ttl)
5. Returns bool result
```

Read operation (Get):

```text
1. Application calls AppTTLStorage().Get(pk, cc)
2. AppTTLStorage prepends app prefix to pk
3. Converts strings to bytes
4. Calls ISysVvmStorage.Get(prefixedPK, cc, &data)
5. Returns (string(data), exists)
```

### Partition key prefix

New prefix constant in pkg/vvm/storage/consts.go:

```go
const pKeyPrefix_AppTTL uint32 = 3 // After pKeyPrefix_VVMLeader(1) and pKeyPrefix_Sequencer(2)
```

Full partition key structure:

```text
[0-3]    uint32   pKeyPrefix_AppTTL (constant = 3)
[4-...]  string   AppQName (e.g., "air")
[...]    string   User-provided pk
```

### Integration points

IAppStructs extension:

```go
type IAppStructs interface {
    // ... existing methods
    AppTTLStorage() IAppTTLStorage
}
```

VVM wiring:

- ISysVvmStorage injected into IAppStructsProvider during VVM initialization
- Each IAppStructs instance gets AppTTLStorage with app-specific prefix

### Related components

- ISysVvmStorage: Low-level VVM storage with TTL support (existing)
- IAppStorageProvider: Provides IAppStorage for sys/vvm application (existing)
- implIElectionsTTLStorage: Similar pattern for elections TTL storage (reference implementation)

