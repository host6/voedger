---
change_id: 2609291131-improve-partition-workspace-recovery
type: feat
issue_url: https://untill.atlassian.net/browse/AIR-5009
domains: [prod]
scope: [apps, storage]
---

# Change request: Improved partition and workspace recovery

Refs:

- [AIR-5009: voedger: imrpove partition and workspace recovery](./issue-AIR-5009.md)

## Why

Partition and workspace sequence state must survive a VVM restart without rebuilding every counter from the complete PLog. Durable log checkpoints let recovery scan only the suffix that can contain newer events, while bounded on-demand workspace recovery prevents one partition from creating an unlimited number of recovery goroutines.

## What

In the production application-processing context:

- Store only the last handled PLog offset in the partition checkpoint.
- Store only the offset of the latest WLog event that allocated non-singleton record IDs in the workspace checkpoint. Do not persist the last WLog offset or record IDs themselves.
- Recover a partition by reading the PLog from its saved handled offset through the end, using the final event to set the next PLog offset, and reapplying that final event.
- After partition recovery, let the requesting command continue to the workspace stage, which starts workspace recovery on demand.
- Recover a workspace by reading its WLog from the saved ID-bearing event offset through the end. Use the actual final event to set the next WLog offset and the latest non-singleton record ID encountered in that suffix to restore allocation.
- If a PLog is empty, start its next offset at `FirstOffset`. If a WLog is empty, start its next offset at `FirstOffset` and its next record ID at `FirstUserRecordID`.
- Return `503 Service Unavailable` while recovery of the requested partition or workspace is already running or if the partition already has `NumWSRecoverers` workspace recoveries running. Log a corresponding message for each case.
- Emit partition recovery lifecycle logs with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid`; emit workspace recovery lifecycle logs with those attributes plus `wsid`.
- Keep the default workspace-recovery limit at four per partition.

## How

Decisions:

- Retain one service-scoped recovery lifecycle per application partition and add a per-partition, on-demand workspace recovery lifecycle.
- Store the partition checkpoint as `{"lastPLogOffset": <offset>}` and store no other PLog checkpoint state.
- Store the workspace checkpoint as `{"lastWLogOffsetWithNewRecordIDs": <offset>}`. The offset identifies the latest checkpointed event that allocated non-singleton record IDs; it is zero when no such event is known.
- Do not use partition IDs or WSIDs as clustering columns. Append the partition ID or WSID, respectively, to the existing checkpoint partition key and use the fixed singleton clustering column `[]byte{1}` for every checkpoint record. Ok to make many small partitions.
- Do not store a record-ID high-water mark and do not extend `IIDGenerator` with a persisted-state accessor.
- Use one built-in asynchronous projector, `ProjectorRecoveryCheckpoint`, for both checkpoint types. Write the current WLog offset only when the event allocates non-singleton record IDs. When `PLogOffset % 100 == 0`, save the current PLog offset.
- On the first request to an unrecovered partition, start only partition recovery and return the existing partition-recovering `503` response.
- Start partition recovery at the persisted `lastPLogOffset`, or `FirstOffset` when the value is absent or zero. Perform one inclusive `ReadPLog(startOffset, ReadToTheEnd)` and retain only the final event returned by the scan.
- If the PLog scan is empty, set `nextPLogOffset = FirstOffset` and publish the recovered partition without reapplying an event. Otherwise, set `nextPLogOffset = lastPLogEventOffset + 1`, reapply that final event, and publish the recovered partition. Do not start workspace recovery from the partition-recovery goroutine.
- Start workspace recovery from the workspace command-processing stage when a subsequent request reaches the recovered partition.
- Admit a workspace recovery only after reserving one of the partition's `NumWSRecoverers` slots. If no slot is available, return `503`, log that the workspace recovery limit was reached, and create neither recovery state nor a waiting goroutine. A later request may try again.
- Deduplicate by WSID. A request for a workspace whose goroutine is still running returns `503`; a completed workspace is admitted immediately. Preserve the existing retained-error/report-and-retry lifecycle for failed recovery attempts.
- Start workspace recovery at the workspace's persisted `lastWLogOffsetWithNewRecordIDs`, or `FirstOffset` when the value is absent or zero. Perform one inclusive `ReadWLog(startOffset, ReadToTheEnd)` and retain the final event offset.
- If the WLog scan is empty, set `nextWLogOffset = FirstOffset` and `nextRecordID = FirstUserRecordID`. Otherwise, set `nextWLogOffset = lastWLogEventOffset + 1` from the actual final event without comparing it with the checkpoint.
- Determine `nextRecordID` only from newly allocated non-singleton CUD record IDs. Singleton IDs have a different scope and must not advance the workspace record-ID generator.
- While scanning from `lastWLogOffsetWithNewRecordIDs` to the WLog end, retain the maximum non-singleton record ID from the latest event that contains one. The stored offset is only the inclusive scan start, never proof that its event is still the latest ID-bearing event or the WLog tail.
- If the complete scanned suffix provides no newly allocated non-singleton record ID, use `FirstUserRecordID` as `nextRecordID`. Otherwise, use the latest found event's maximum non-singleton record ID plus one.
- Log recovery admission and failure responses through `cp.error` with `partition <partitionID>:` or `workspace <wsid>:` prefixes.
- Log partition recovery start, PLog-read failure, and completion with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid=<partitionID>`.
- Log workspace recovery start, initial WLog suffix-read failure, and completion with the partition recovery attributes plus `wsid=<workspaceID>`.
- Treat `NumWSRecoverers` as the exact upper bound. The default VVM configuration sets it to four; an explicit zero permits no workspace recovery and therefore yields `503` for an unrecovered workspace.
- Keep partition and workspace recovery tied to the service context, ignore stale attempt completion after partition replacement, and join recovery goroutines during command-service shutdown.

Do not store the last WLog offset alongside the ID-bearing offset. The ID-bearing checkpoint can lag: another ID-bearing event and arbitrary later events may already exist when the VVM stops. Treating the checkpoint as authoritative would restore stale sequence state. Reading from that checkpoint through the WLog end always discovers the actual tail and any later allocation, so storing the tail separately would duplicate data that recovery must verify anyway.

```text
              contains new IDs            contains new IDs
                 1001..1003                  1004..1006
                      |        no new IDs         |                          no new IDs
                      v             v             v                               v
WLog: ... ---------- 41 ---------- 42 ---------- 43 ---------------------------- 44 ---------- END
                      ^                           ^                               ^
                      |                           |                               |
             stored checkpoint = 41        actual lastRecordID=1006        actual last WLog offset = 44
             scan starts here; it is       computed nextRecordID=1007      nextWLogOffset = 45
             not the point of truth                                               |
                      |                                                           |
                      |<--------------- ReadWLog(41, ReadToTheEnd) -------------->|

Latest ID-bearing event found by the scan = 43
nextRecordID = max(1004..1006) + 1 = 1007

Empty PLog: START -> END    nextPLogOffset = FirstOffset
Empty WLog: START -> END    nextWLogOffset = FirstOffset, nextRecordID = FirstUserRecordID
```

Out of scope:

- Changing workspace-ID allocation or Sequence Trust Level write policy.

References (internal):

- [command/impl.go](../../../pkg/processors/command/impl.go)
- [command/provide.go](../../../pkg/processors/command/provide.go)
- [sys/checkpoints/checkpoints.go](../../../pkg/sys/checkpoints/checkpoints.go)
- [storage/impl_recoverycheckpoint.go](../../../pkg/vvm/storage/impl_recoverycheckpoint.go)
- [istructs/events-types.go](../../../pkg/istructs/events-types.go)
- [vvm/types.go](../../../pkg/vvm/types.go)
- [recovery logging contract](../../specs/prod/apps/logging--td.md#command-processor)
- [AIR-5009 recovery requirements](./issue-AIR-5009.md)

References (external):

- [post-removal sequence-storage baseline in PR #4649](https://github.com/voedger/voedger/pull/4649)
- [partition-recovery performance and shared-storage scope in AIR-4959](https://untill.atlassian.net/browse/AIR-4959)

## Technical design

- [x] update: [apps/logging--td.md](../../specs/prod/apps/logging--td.md)
  - document: partition and workspace recovery admission and failure messages emitted through `cp.error`
  - document: partition and workspace recovery lifecycle stages, context attributes, and completion messages

## Construction

### Tests

- [x] create: [checkpoints/checkpoints_test.go](../../../pkg/sys/checkpoints/checkpoints_test.go)
  - verify that one projector stores `lastPLogOffset` when `PLogOffset % 100 == 0`
  - verify that an event allocating non-singleton record IDs stores `lastWLogOffsetWithNewRecordIDs`, while singleton, existing-record, and CUD-free events do not write a workspace checkpoint
  - verify storage-error propagation and registration of the single standard asynchronous projector

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - verify one inclusive PLog read from the saved `lastPLogOffset` to the end
  - verify that partition recovery does not start workspace recovery and that a subsequent request starts it from the command stage
  - verify one inclusive WLog read from the workspace's saved `lastWLogOffsetWithNewRecordIDs` to the end
  - verify that the final WLog event determines the next WLog offset
  - verify that a later ID-bearing event in the scanned suffix, rather than the stored checkpoint event, determines the next record ID
  - verify that singleton-only and CUD-free events after the checkpoint still contribute to the actual WLog tail
  - verify that a history without non-singleton CUDs starts allocation at `FirstUserRecordID`
  - verify that empty PLog and WLog scans start offsets at `FirstOffset` and an empty WLog starts record allocation at `FirstUserRecordID`
  - verify one recovery attempt per WSID, `503` while it is running, and retained failure/retry behavior
  - verify that the concurrency limit admits at most `NumWSRecoverers`, returns `503` without queueing excess work, logs the limit condition, and permits a later retry after a slot is released
  - verify distinct log messages for partition-in-progress and workspace-in-progress responses
  - verify that a partition recovery admission-limit result is translated to `503 Service Unavailable` rather than a panic
  - verify that a zero limit admits no workspace recovery

- [x] create: [command/recover_manager_test.go](../../../pkg/processors/command/recover_manager_test.go)
  - directly verify generic recovery-manager startup, ready lookup, duplicate suppression, retained failures, retries, admission limits, hooks, cancellation, reset, stale-completion rejection, recovered-value projection, and shutdown clearing
  - relocate stale-completion coverage from the partition-manager integration-style test

- [x] create: [command/checkpoints_test.go](../../../pkg/processors/command/checkpoints_test.go)
  - provide a thread-safe last-write-wins checkpoint test double using `lastPLogOffset` and `lastWLogOffsetWithNewRecordIDs`
  - implement partition and workspace checkpoint reads, writes, forced setup, reset, and write-order observation for command recovery tests

- [x] create: [command/recovery_test_utils_test.go](../../../pkg/processors/command/recovery_test_utils_test.go)
  - centralize deterministic test support for asynchronous partition and workspace recovery
  - provide recovery-attempt gates, injected failures, completion waits, and inclusive suffix PLog/WLog read observations
  - provide a retrying request sender for command tests whose subject is unrelated to lazy recovery

- [x] delete: [command/test_utils.go](../../../pkg/processors/command/test_utils.go)
  - replace the partition-only recovery test controls with the generic test-only utilities in `recovery_test_utils_test.go`

- [x] create: [storage/impl_recoverycheckpoint_test.go](../../../pkg/vvm/storage/impl_recoverycheckpoint_test.go)
  - verify single-field PLog and WLog JSON values named `lastPLogOffset` and `lastWLogOffsetWithNewRecordIDs`, including missing and malformed values and last-write-wins replacement
  - verify application/partition/workspace isolation through partition-key suffixes and the fixed singleton clustering column `[]byte{1}`

- [x] update: [storage/consts_test.go](../../../pkg/vvm/storage/consts_test.go)
  - preserve fixed sequence-storage prefix values

- [x] update: [actualizers/impl_helpers_test.go](../../../pkg/processors/actualizers/impl_helpers_test.go)
  - extend PLog event mocks with handling-partition and PLog-offset accessors used by the checkpoint projector

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - verify `lastWLogOffsetWithNewRecordIDs` across sequential VVM instances sharing storage
  - verify through the next successful CUD response that restart preserves each workspace's next WLog offset and record ID
  - verify through the next successful CUD response that a singleton tail advances the WLog offset but not the record ID
  - verify through the next successful CUD response that a tail event for another workspace in the same partition does not advance the first workspace's WLog offset or record ID

- [x] update: [vit/utils.go](../../../pkg/vit/utils.go)
  - retain the shared-storage two-VVM lifecycle helper used by the recovery integration test
  - add lifecycle hooks that inspect stable shared storage after each VVM stops

### Checkpoint contracts and storage

- [x] update: [istructs/events-types.go](../../../pkg/istructs/events-types.go)
  - expose handling partition and PLog offset on persisted PLog events for checkpoint projection
  - keep `IIDGenerator` free of checkpoint-specific record-ID accessors

- [x] update: [storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - retain the sequence-storage prefix values and document partition ID and WSID as partition-key suffixes rather than clustering columns

- [x] create: [storage/impl_recoverycheckpoint.go](../../../pkg/vvm/storage/impl_recoverycheckpoint.go)
  - append partition ID or WSID to the corresponding application partition key and use `[]byte{1}` as the clustering column
  - encode only `lastPLogOffset` for PLog checkpoints and only `lastWLogOffsetWithNewRecordIDs` for WLog checkpoints
  - report missing values as absent, reject malformed values, and overwrite unconditionally

- [x] update: [storage/provide.go](../../../pkg/vvm/storage/provide.go)
  - construct the recovery-checkpoint adapter over shared system-VVM storage

- [ ] create: [checkpoints/checkpoints.go](../../../pkg/sys/checkpoints/checkpoints.go)
  - define a PLog checkpoint containing only `lastPLogOffset` and a WLog checkpoint containing only `lastWLogOffsetWithNewRecordIDs`
  - register one built-in asynchronous recovery-checkpoint projector
  - persist the WLog offset only for non-singleton allocations and persist the PLog offset when `PLogOffset % 100 == 0`

- [x] update: [parser/impl_analyse.go](../../../pkg/parser/impl_analyse.go)
  - resolve the built-in generic Command trigger used by the recovery-checkpoint projector

- [x] update: [parser/impl_build.go](../../../pkg/parser/impl_build.go)
  - map the generic Command trigger to command events

- [ ] update: [sys/sys.vsql](../../../pkg/sys/sys.vsql)
  - replace both built-in recovery-checkpoint declarations with one projector for command, CUD, and ODoc events

- [ ] update: [pkg/sys/sys.vsql](../../../pkg/sys/it/testdata/apps/test2.app1/image/pkg/sys/sys.vsql)
  - mirror the single built-in recovery-checkpoint projector declaration in the generated integration-test schema

- [ ] update: [sys/sysprovide/provide.go](../../../pkg/sys/sysprovide/provide.go)
  - register the single checkpoint projector with stateless resources

### Command recovery

- [x] update: [command/consts.go](../../../pkg/processors/command/consts.go)
  - define shared errors for recovery-in-progress, concurrency-limit, and retained-failure outcomes

- [x] create: [command/recover_manager.go](../../../pkg/processors/command/recover_manager.go)
  - implement the generic keyed recovery lifecycle shared by partitions and workspaces
  - reserve optional concurrency slots before creating attempts, retain failures for one reporting request, and publish successful recovered values
  - reject stale completions after reset and join all workers before clearing state during shutdown

- [x] update: [command/types.go](../../../pkg/processors/command/types.go)
  - define generic recovery values, attempts, hooks, and manager state for absent, recovering, failed, and ready units
  - define partition and workspace keys and manager wrappers over the shared generic recovery manager
  - retain application identity and structures in recovered partitions for asynchronous WLog recovery

- [ ] update: [command/impl.go](../../../pkg/processors/command/impl.go)
  - recover the partition with one inclusive PLog suffix scan and use only its final event
  - publish the recovered partition without starting workspace recovery from the partition-recovery goroutine
  - start workspace recovery on demand from the command pipeline's workspace stage
  - recover the WLog end offset and latest non-singleton record ID with one inclusive scan from `lastWLogOffsetWithNewRecordIDs` through the actual tail
  - initialize empty PLog and WLog offsets with `FirstOffset` and an empty WLog record-ID generator with `FirstUserRecordID`
  - ignore singleton IDs and retain `FirstUserRecordID` when the complete WLog contains no newly allocated non-singleton CUD
  - let the generic manager deduplicate attempts, enforce admission, run workers and hooks, retain failures, reject stale completions, publish results, and account for worker shutdown
  - expose the lifecycle through one generic `getOrStart` operation returning only the recovered value or an error, without a separate state enum or decision object
  - inject the partition recovery function once into the partition manager instead of forwarding it through each partition lookup
  - keep recovery startup, logging, hooks, and HTTP error policy in the thin partition and workspace managers
  - reserve a slot before starting a goroutine and return `503` both when full and for an in-progress WSID
  - translate a generic partition recovery-limit result to a logged `503 Service Unavailable` response rather than panicking
  - prefix recovery admission and failure messages with the affected partition ID or workspace ID
  - attach `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid` to partition recovery lifecycle logs, and add `wsid` to workspace recovery lifecycle logs
  - log workspace recovery start, initial WLog suffix-read failure, and completion using dedicated `cp.workspace_recovery.*` stages
  - remove recovery-time checkpoint writes and all persisted record-ID handling

- [x] update: [command/provide.go](../../../pkg/processors/command/provide.go)
  - inject checkpoint storage and the exact workspace-recovery limit into command-service construction
  - do not reinterpret an explicit zero limit as the default
  - preserve service-context cancellation and worker joining during shutdown

### VVM configuration and wiring

- [x] update: [vvm/consts.go](../../../pkg/vvm/consts.go)
  - define the default per-partition workspace recovery concurrency as four

- [x] update: [vvm/types.go](../../../pkg/vvm/types.go)
  - expose `NumWSRecoverers` in VVM configuration

- [x] update: [vvm/impl_cfg.go](../../../pkg/vvm/impl_cfg.go)
  - initialize `NumWSRecoverers` from its default

- [x] update: [vvm/provide.go](../../../pkg/vvm/provide.go)
  - wire shared checkpoint storage and workspace-recovery concurrency into stateless resources and the command service

- [x] regenerate: [vvm/wire_gen.go](../../../pkg/vvm/wire_gen.go)
  - reflect the updated Wire provider graph

## Quick start

The default configuration permits four concurrent workspace recoveries per partition. Override it before starting the VVM when a deployment needs a different hard limit:

```go
cfg := vvm.NewVVMDefaultConfig()
cfg.NumWSRecoverers = 8
```

Setting `NumWSRecoverers` to zero disables workspace recovery admission; requests for unrecovered workspaces receive `503 Service Unavailable` and the limit condition is logged.
