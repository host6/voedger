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

Partition and workspace sequence state must survive a VVM restart without rebuilding every counter from the complete PLog. Durable handled-offset checkpoints let recovery scan only the log suffix that can contain newer events, while bounded on-demand workspace recovery prevents one partition from creating an unlimited number of recovery goroutines.

## What

In the production application-processing context:

- Store only the last handled log offset in each recovery checkpoint. Do not persist record IDs.
- Recover a partition by reading the PLog from its saved handled offset through the end, using the final event to set the next PLog offset, and reapplying that final event.
- After partition recovery, let the requesting command continue to the workspace stage, which starts workspace recovery on demand.
- Recover a workspace by reading its WLog from its saved handled offset through the end. Use the final event to set the next WLog offset, and rewind in 10-event windows when needed to find the latest non-singleton record ID.
- Return `503 Service Unavailable` while recovery of the requested partition or workspace is already running or if the partition already has `NumWSRecoverers` workspace recoveries running. Log a corresponding message for each case.
- Emit partition recovery lifecycle logs with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid`; emit workspace recovery lifecycle logs with those attributes plus `wsid`.
- Keep the default workspace-recovery limit at four per partition.

## Constraints

- Do not modify any documentation changed by [PR #4651](https://github.com/voedger/voedger/pull/4651) in this pull request. Update that documentation in a separate follow-up pull request after all related pull requests are approved.
- Design and implement against the expected result of [PR #4649](https://github.com/voedger/voedger/pull/4649): the currently unused `isequencer` implementation is removed, while the sequence trust-level constants retained in `pkg/isequencer/consts.go` remain available.

## How

Decisions:

- Retain one service-scoped recovery lifecycle per application partition and add a per-partition, on-demand workspace recovery lifecycle.
- Store checkpoint values as JSON objects with exactly one field: `lastHandledWLogOffset` and `lastHandledPLogOffset` for workspace and PLog storage respectively.
- Store the partition checkpoint in `SeqStorage_Part_PLog_offset`, keyed by application, using the partition ID as the clustering column. The value is `{"lastHandledPLogOffset": <offset>}`.
- Store all workspace checkpoints for an application in `SeqStorage_WS_sequences`, with the WSID encoded in the clustering columns and `{"lastHandledWLogOffset": <offset>}` as the value.
- Do not store a record-ID high-water mark and do not extend `IIDGenerator` with a persisted-state accessor.
- Use the built-in asynchronous recovery-checkpoint projector to save the current event's WLog offset first and its PLog offset second. This ordering prevents the partition checkpoint from covering an event whose workspace checkpoint has not been stored.
- On the first request to an unrecovered partition, start only partition recovery and return the existing partition-recovering `503` response.
- Start partition recovery at the persisted `lastHandledPLogOffset`, or `FirstOffset` when the value is absent or zero. Perform one inclusive `ReadPLog(startOffset, ReadToTheEnd)` and retain only the final event returned by the scan.
- Set `nextPLogOffset = lastPLogEventOffset + 1` from that final event without comparing it with any earlier or persisted value. Reapply the final event and publish the recovered partition. Do not start workspace recovery from the partition-recovery goroutine.
- Start workspace recovery from the workspace command-processing stage when a subsequent request reaches the recovered partition.
- Admit a workspace recovery only after reserving one of the partition's `NumWSRecoverers` slots. If no slot is available, return `503`, log that the workspace recovery limit was reached, and create neither recovery state nor a waiting goroutine. A later request may try again.
- Deduplicate by WSID. A request for a workspace whose goroutine is still running returns `503`; a completed workspace is admitted immediately. Preserve the existing retained-error/report-and-retry lifecycle for failed recovery attempts.
- Start workspace recovery at the workspace's persisted `lastHandledWLogOffset`, or `FirstOffset` when the value is absent or zero. Perform one inclusive `ReadWLog(startOffset, ReadToTheEnd)` and retain the final event offset.
- Set `nextWLogOffset = lastWLogEventOffset + 1` from the final WLog event without comparing it with any earlier or persisted value.
- Determine `nextRecordID` only from newly allocated non-singleton CUD record IDs. Singleton IDs have a different scope and must not advance the workspace record-ID generator.
- While scanning from `lastHandledWLogOffset` to the WLog end, retain the maximum non-singleton record ID from the latest event that contains one. If none is found, scan backward from `lastHandledWLogOffset` in non-overlapping 10-event windows until such an event is found or `FirstOffset` is reached.
- If no newly allocated non-singleton CUD exists between `FirstOffset` and the WLog end, use `FirstUserRecordID` as `nextRecordID`. Otherwise, use the found event's maximum non-singleton record ID plus one. Backward scans do not change `nextWLogOffset`.
- Log recovery admission and failure responses through `cp.error` with `partition <partitionID>:` or `workspace <wsid>:` prefixes.
- Log partition recovery start, PLog-read failure, and completion with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid=<partitionID>`.
- Log workspace recovery start, initial WLog suffix-read failure, and completion with the partition recovery attributes plus `wsid=<workspaceID>`.
- Treat `NumWSRecoverers` as the exact upper bound. The default VVM configuration sets it to four; an explicit zero permits no workspace recovery and therefore yields `503` for an unrecovered workspace.
- Keep partition and workspace recovery tied to the service context, ignore stale attempt completion after partition replacement, and join recovery goroutines during command-service shutdown.

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

## Construction

### Tests

- [x] update: [checkpoints/checkpoints_test.go](../../../pkg/sys/checkpoints/checkpoints_test.go)
  - verify that the projector stores the event's handled PLog and WLog offsets rather than next offsets
  - verify workspace-before-partition write ordering and failure behavior

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - verify one inclusive PLog read from the saved handled offset to the end
  - verify that partition recovery does not start workspace recovery and that a subsequent request starts it from the command stage
  - verify one inclusive WLog read from the workspace's saved handled offset to the end
  - verify that the final WLog event determines the next WLog offset
  - verify that singleton-only and CUD-free tails rewind in 10-event windows to find the latest non-singleton record ID
  - verify that a history without non-singleton CUDs starts allocation at `FirstUserRecordID`
  - verify one recovery attempt per WSID, `503` while it is running, and retained failure/retry behavior
  - verify that the concurrency limit admits at most `NumWSRecoverers`, returns `503` without queueing excess work, logs the limit condition, and permits a later retry after a slot is released
  - verify distinct log messages for partition-in-progress and workspace-in-progress responses
  - verify that a partition recovery admission-limit result is translated to `503 Service Unavailable` rather than a panic
  - verify that a zero limit admits no workspace recovery

- [x] create: [command/recover_manager_test.go](../../../pkg/processors/command/recover_manager_test.go)
  - directly verify generic recovery-manager startup, ready lookup, duplicate suppression, retained failures, retries, admission limits, hooks, cancellation, reset, stale-completion rejection, recovered-value projection, and shutdown clearing
  - relocate stale-completion coverage from the partition-manager integration-style test

- [x] update: [command/checkpoints_test.go](../../../pkg/processors/command/checkpoints_test.go)
  - provide a thread-safe last-write-wins checkpoint test double using handled-offset values only

- [x] update: [command/test_utils.go](../../../pkg/processors/command/test_utils.go)
  - add deterministic PLog/WLog read observations and independent partition/workspace recovery gates

- [x] update: [storage/impl_recoverycheckpoint_test.go](../../../pkg/vvm/storage/impl_recoverycheckpoint_test.go)
  - verify exact single-field JSON values, missing and malformed values, and last-write-wins replacement
  - verify application/partition isolation and WSID clustering-column isolation

- [x] update: [storage/consts_test.go](../../../pkg/vvm/storage/consts_test.go)
  - preserve fixed sequence-storage prefix values

- [x] update: [istructsmem/idgenerator_test.go](../../../pkg/istructsmem/idgenerator_test.go)
  - remove checkpoint-specific last-record-ID accessor coverage

- [x] update: [actualizers/impl_helpers_test.go](../../../pkg/processors/actualizers/impl_helpers_test.go)
  - extend PLog event mocks with handling-partition and PLog-offset accessors used by the checkpoint projector

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - verify handled-offset checkpoint values across sequential VVM instances sharing storage
  - verify WLog offset and record-ID continuity when record IDs are reconstructed from WLog CUDs

- [x] update: [vit/utils.go](../../../pkg/vit/utils.go)
  - retain the shared-storage two-VVM lifecycle helper used by the recovery integration test

### Checkpoint contracts and storage

- [x] update: [istructs/events-types.go](../../../pkg/istructs/events-types.go)
  - expose handling partition and PLog offset on persisted PLog events for checkpoint projection
  - keep `IIDGenerator` free of checkpoint-specific record-ID accessors

- [x] update: [istructsmem/idgenerator.go](../../../pkg/istructsmem/idgenerator.go)
  - remove the last-record-ID accessor and its checkpoint-only helper plumbing

- [x] update: [storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - retain the sequence-storage prefix values and identify their partition-offset and WSID clustering-column roles

- [x] update: [storage/impl_recoverycheckpoint.go](../../../pkg/vvm/storage/impl_recoverycheckpoint.go)
  - store partition JSON under an application key with partition ID in the clustering columns
  - store workspace JSON under an application key with WSID in the clustering columns
  - encode only `lastHandledPLogOffset` or `lastHandledWLogOffset`, report missing values as absent, reject malformed values, and overwrite unconditionally

- [x] update: [storage/provide.go](../../../pkg/vvm/storage/provide.go)
  - construct the recovery-checkpoint adapter over shared system-VVM storage

- [x] update: [checkpoints/checkpoints.go](../../../pkg/sys/checkpoints/checkpoints.go)
  - define single-field partition and workspace handled-offset checkpoint contracts
  - register the built-in asynchronous projector and persist workspace before partition for each event

- [x] update: [parser/impl_analyse.go](../../../pkg/parser/impl_analyse.go)
  - resolve the built-in generic Command trigger used by the recovery-checkpoint projector

- [x] update: [parser/impl_build.go](../../../pkg/parser/impl_build.go)
  - map the generic Command trigger to command events

- [x] update: [sys/sys.vsql](../../../pkg/sys/sys.vsql)
  - declare the built-in recovery-checkpoint projector for command, CUD, and ODoc events

- [x] update: [sys/sysprovide/provide.go](../../../pkg/sys/sysprovide/provide.go)
  - register the checkpoint projector with stateless resources

### Command recovery

- [x] update: [command/types.go](../../../pkg/processors/command/types.go)
  - add a generic recovery manager that maintains absent, recovering, failed, and ready units in one synchronized map, using an embedded non-nil recovered value to represent readiness
  - use the same manager for application partitions and for the per-partition workspace collection
  - keep the shared application structures needed by asynchronous WLog recovery
  - let each generic manager own its attempt hooks and recovery lifecycle while workspace managers share the service worker wait group

- [x] update: [command/impl.go](../../../pkg/processors/command/impl.go)
  - recover the partition with one inclusive PLog suffix scan and use only its final event
  - publish the recovered partition without starting workspace recovery from the partition-recovery goroutine
  - start workspace recovery on demand from the command pipeline's workspace stage
  - recover the WLog end offset with one inclusive suffix scan and rewind in 10-event windows when no non-singleton CUD ID is found
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
