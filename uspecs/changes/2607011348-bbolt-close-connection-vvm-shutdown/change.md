---
change_id: 2607011348-bbolt-close-connection-vvm-shutdown
type: fix
issue_url: https://untill.atlassian.net/browse/AIR-4360
domains: [prod]
scope: [storage]
---

# Change request: Close bbolt DB connection on VVM shutdown

Refs:

- [AIR-4360: bbolt: close the connection on VVM shutdown](./issue-AIR-4360.md)

## Why

The bbolt storage driver opens a `bolt.DB` connection per application but never closes it, so on VVM shutdown the underlying database files stay open. Because an open (memory-mapped) bbolt file cannot be deleted, a test database — which may have grown to 128 Mb during the test — is left on disk after the test finishes. The size is a consequence of the leaked open handle, not a separate problem to fix.

## What

Symptom: after VVM shutdown the bbolt database files stay open (memory-mapped), so they cannot be deleted and a test database that grew to 128 Mb is left on disk.

```text
VVM shutdown
      |
      v
IAppStorageProvider.Stop()   (pkg/istorage/provider/impl.go)
      |
      v
appStorageFactory.StopGoroutines()   <-- fault: cancels the background cleaner and waits on the WaitGroup, but never Close()s the opened bolt.DB connections
      |
      v
opened bolt.DB handles remain open and keep the db file mapped/locked
      |
      v
db file stays open, so it cannot be deleted and is left on disk   (symptom)
```

Corrected behavior: on `IAppStorageProvider.Stop()` the bbolt driver closes every opened `bolt.DB` connection so each database file is released and can be deleted.

Using a storage handle after `Stop()` is unsupported (shutdown-only): `Stop()` waits for in-flight transactions to finish, and any later operation on a closed connection returns bbolt's `ErrDatabaseNotOpen`.

## How

Decisions:

- Close the opened `bolt.DB` from the per-storage background goroutine via `defer` on context cancellation (`backgroundCleaner` in `pkg/istorage/bbolt/impl.go`), reusing the existing `WaitGroup` so `StopGoroutines()` already blocks until every connection is closed
- Log any `db.Close()` error instead of returning it, consistent with the goroutine's existing error logging
- Keep the change confined to the bbolt driver: no changes to the `IAppStorage` / `IAppStorageFactory` interfaces or to other drivers

Out of scope:

- Deleting the database file on disk (only the connection is closed; releasing the handle is what lets callers delete it)
- Reducing on-disk database size or compaction (the 128 Mb size is a consequence of the leaked handle, not a separate goal)
- The `mem` and `cas` storage drivers

References:

- [bbolt driver: factory, AppStorage, StopGoroutines, backgroundCleaner](../../../pkg/istorage/bbolt/impl.go)
- [provider Stop() delegating to StopGoroutines()](../../../pkg/istorage/provider/impl.go)
- [bbolt driver tests](../../../pkg/istorage/bbolt/impl_test.go)

## Construction

### Tests

- [ ] update: [istorage/bbolt/impl_test.go](../../../pkg/istorage/bbolt/impl_test.go)
  - extend `TestAppStorageFactory_StopGoroutines`: keep the returned `*appStorageType`, and after `storageProvider.Stop()` assert its `db` is closed cross-platform via `require.ErrorIs(impl.db.View(func(*bolt.Tx) error { return nil }), bolt.ErrDatabaseNotOpen)` (do not rely on `os.Remove`, which succeeds on Unix even with an open handle)
  - the assertion after `Stop()` returns also proves closing is synchronous (relies on the existing `WaitGroup`)

### Implementation

- [ ] update: [istorage/bbolt/impl.go](../../../pkg/istorage/bbolt/impl.go)
  - `backgroundCleaner`: add a `defer` that closes `s.db` when the goroutine exits on context cancellation, so `StopGoroutines()` (`cancel()` + `wg.Wait()`) blocks until every connection is closed
  - register the close `defer` after the existing `defer wg.Done()` so that, by LIFO order, `s.db.Close()` runs before `wg.Done()` — guaranteeing `wg.Wait()` returns only once the connection is actually closed
  - log any `db.Close()` error via `logger.Error`, consistent with the goroutine's existing cleanup error logging
