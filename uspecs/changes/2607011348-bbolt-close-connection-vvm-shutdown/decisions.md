# Decisions: Close bbolt DB connection on VVM shutdown

## Ambiguity: is reducing/compacting on-disk DB size in scope, or only closing the connection?

Decision: Close-only — releasing the `bolt.DB` connection on `IAppStorageProvider.Stop()` is the whole fix; the 128 Mb size is framed as a consequence of the leaked open handle, and on-disk size reduction/compaction is out of scope

- Pros: minimal, single responsibility; directly resolves "db not deleted after the test" (an open, memory-mapped bbolt file cannot be deleted); matches the issue's literal "close the bbolt session on `IAppStorageProvider.Stop()`"; no runtime behavior change during normal operation
- Cons: does not shrink long-lived databases; leaves file bloat unaddressed for long-running deployments
- Confidence: high

Alternatives:

1. Close + compact (also reduce on-disk size via compaction on close or periodically)
   - Pros: addresses long-term file bloat in addition to the leak
   - Cons: larger scope; changes shutdown cost and adds failure modes; unrelated to the "not deleted" root cause; better handled as a separate ticket
   - Confidence: low

## Uncertainty: behavior when a caller uses an already-obtained IAppStorage after Stop()

Decision: Accept the low-level error on post-`Stop()` use — using a storage handle after `Stop()` is unsupported (shutdown-only); `Stop()` waits for in-flight transactions and later operations return bbolt's `ErrDatabaseNotOpen`

- Pros: simplest; consistent with shutdown semantics and with the provider already refusing new `AppStorage()` calls via `ErrStoppingState`; `bolt.Close()` waits for in-flight transactions so there is no corruption; no per-operation state
- Cons: a late caller observes a raw bbolt error rather than a domain-level one
- Confidence: high

Alternatives:

1. Guard with a defined "stopped" error on the storage handle
   - Pros: cleaner, driver-owned contract instead of leaking bbolt's internal error
   - Cons: adds a closed-flag and a check on every operation; scope and behavior change beyond the leak fix
   - Confidence: low
