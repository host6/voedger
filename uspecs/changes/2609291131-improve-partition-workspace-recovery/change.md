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

Partition and workspace sequence state must survive a VVM restart without rebuilding every counter from the complete log.

## What

In the production application-processing context:

- Store only the last handled PLog offset in the partition checkpoint.
- Store one WLog ID-bearing offset: the offset of the most recent ID-bearing event observed by the checkpoint projector. Do not persist the last WLog offset or record IDs themselves.
- Recover a partition by reading the PLog from its saved handled offset through the end, using the final event to set the next PLog offset, and reapplying that final event.
- After partition recovery, let the requesting command continue to the workspace stage, which starts workspace recovery on demand.
- Recover a workspace by reading its WLog inclusively from the WLog ID-bearing offset through the end. Use the actual final event to set the next WLog offset and the latest ID-bearing event encountered in the complete suffix to restore record-ID allocation.
- Treat a stored checkpoint as invalid when its inclusive suffix scan invokes no callback, its first callback has a different offset, or a workspace checkpoint event did not allocate a non-singleton record ID. Log an error and recover from `FirstOffset` instead of accepting counters derived from an empty or incomplete suffix.
- If a PLog is empty, start its next offset at `FirstOffset`. If a WLog is empty, start its next offset at `FirstOffset` and its next record ID at `FirstUserRecordID`.
- Return `503 Service Unavailable` while recovery of the requested partition or workspace is already running or if the partition already has `NumWSRecoverers` workspace recoveries running. Log a corresponding message for each case.
- Emit partition recovery lifecycle logs with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid`; emit workspace recovery lifecycle logs with those attributes plus `wsid`.
- Keep the default workspace-recovery limit at four per partition.

## How

### Decisions

#### Key decisions

- Store the offset of the latest ID-bearing WLog event seen by the checkpoint projector.
- The stored offset is only a safe scan accelerator. It must not become an authoritative copy of either recovered value.
- Store the offset of the latest PLog event seen by the checkpoint projector.
- Term: this stored set of offsets is `checkpoint`, `recovery checkpoint`

#### Key flow

1. During normal processing, `ProjectorRecoveryCheckpoint` stores the current PLog offset every 100 PLog events and replaces the workspace checkpoint whenever it sees a later ID-bearing WLog event.
2. After restart, the first request to an unrecovered partition starts partition recovery from its partition checkpoint and receives `503 Service Unavailable`.
3. Partition recovery validates the checkpoint, scans inclusively through the actual PLog tail, restores the next PLog offset, reapplies the final event when one exists, and publishes the recovered partition without starting workspace recovery.
4. The next request reaches the workspace stage, starts recovery of its workspace from the workspace checkpoint, and receives `503 Service Unavailable`.
5. Workspace recovery validates the checkpoint, scans inclusively through the actual WLog tail, restores the next WLog offset and record ID, and publishes the recovered workspace.
6. A subsequent request uses the recovered partition and workspace and proceeds normally.

#### Checkpoint data and persistence

- Store the partition checkpoint as `{"lastPLogOffset": <offset>}` and store no other PLog checkpoint state.
- Store the workspace checkpoint as `{"wlogIDBearingOffset": <offset>}`. The field identifies the latest ID-bearing event seen by the checkpoint projector; it is zero when the projector has not seen such an event and is never authoritative sequence state.
- Do not additionally store the last WLog offset. The asynchronous projector may lag the WLog, so its latest seen ID-bearing offset is not necessarily the latest ID-bearing event in the log. Recovery must still scan from the checkpoint through the tail, and a separately stored last offset does not safely shorten that scan.
- Do not use partition IDs or WSIDs as clustering columns. Append the partition ID or WSID, respectively, to the existing checkpoint partition key and use the fixed singleton clustering column `[]byte{1}` for every checkpoint record. Ok to make many small partitions.
- Do not store a record-ID high-water mark and do not extend `IIDGenerator` with a persisted-state accessor.
- Use one built-in asynchronous projector, `ProjectorRecoveryCheckpoint`, for both checkpoint types. Whenever the projector sees an event that allocates non-singleton record IDs, replace `wlogIDBearingOffset` with that event's offset so the checkpoint identifies the latest ID-bearing event seen by the projector. Recovery correctness must not depend on the projector having reached the WLog tail. When `PLogOffset % 100 == 0`, save the current PLog offset.

#### Partition recovery

- On the first request to an unrecovered partition, start only partition recovery and return the existing partition-recovering `503` response.
- Start partition recovery at the persisted `lastPLogOffset`, or `FirstOffset` when the value is absent or zero. Perform an inclusive `ReadPLog(startOffset, ReadToTheEnd)` and retain only the final event returned by the scan.
- When a partition checkpoint is present, require the first returned PLog event offset to equal `lastPLogOffset`. No returned event or a different first offset means that the checkpoint does not identify an existing event.
- If the partition checkpoint is invalid, abort the suffix scan, log `cp.partition_recovery.checkpoint.invalid` error, and repeat the inclusive scan from `FirstOffset`. Never perform more than these two scans and do not rewrite the checkpoint during recovery.
- If the PLog scan from `FirstOffset` is empty, set `nextPLogOffset = FirstOffset` and publish the recovered partition without reapplying an event. Otherwise, set `nextPLogOffset = lastPLogEventOffset + 1`, reapply that final event, and publish the recovered partition. Do not start workspace recovery from the partition-recovery goroutine.

#### Workspace recovery

- Start workspace recovery from the workspace command-processing stage when a subsequent request reaches the recovered partition.
- Start workspace recovery at the persisted `wlogIDBearingOffset`, or `FirstOffset` when it is absent or less than `FirstOffset`. Perform one inclusive `ReadWLog(startOffset, ReadToTheEnd)`, retaining the final event offset and the latest non-singleton record ID encountered anywhere in the suffix.
- When a workspace checkpoint is present, require the first callback to report exactly `wlogIDBearingOffset` and require that event to allocate at least one non-singleton record ID. No callback, a different first callback offset, or a non-ID-bearing checkpoint event makes the checkpoint invalid.
- If the workspace checkpoint is invalid, abort the suffix scan, log `cp.workspace_recovery.checkpoint.invalid`, and repeat one inclusive scan from `FirstOffset`. The fallback scan restores both counters; never perform more than one fallback scan and do not rewrite the checkpoint during recovery.
- If the WLog scan from `FirstOffset` is empty, set `nextWLogOffset = FirstOffset` and `nextRecordID = FirstUserRecordID`. Otherwise, set `nextWLogOffset = lastWLogEventOffset + 1` from the actual final event without comparing it with the checkpoint.
- Determine `nextRecordID` only from newly allocated non-singleton CUD record IDs. Singleton IDs have a different scope and must not advance the workspace record-ID generator.
- While scanning from `wlogIDBearingOffset` to the WLog end, retain the maximum non-singleton record ID from the latest event that contains one. The stored offset is the latest ID-bearing event seen by the projector, but it is only an inclusive recovery start and does not prove that the projector had reached the latest ID-bearing event or the WLog tail.
- If the complete scanned suffix provides no newly allocated non-singleton record ID, use `FirstUserRecordID` as `nextRecordID`. Otherwise, use the latest found event's maximum non-singleton record ID plus one.

#### Recovery coordination and admission

- Retain one service-scoped recovery lifecycle per application partition and add a per-partition, on-demand workspace recovery lifecycle.
- Admit a workspace recovery only after reserving one of the partition's `NumWSRecoverers` slots. If no slot is available, return `503`, log that the workspace recovery limit was reached, and create neither recovery state nor a waiting goroutine. A later request may try again.
- Deduplicate by WSID. A request for a workspace whose goroutine is still running returns `503`; a completed workspace is admitted immediately. Preserve the existing retained-error/report-and-retry lifecycle for failed recovery attempts.
- Treat `NumWSRecoverers` as the exact upper bound. The default VVM configuration sets it to four; an explicit zero permits no workspace recovery and therefore yields `503` for an unrecovered workspace.
- Keep partition and workspace recovery tied to the service context, ignore stale attempt completion after partition replacement, and join recovery goroutines during command-service shutdown.

#### Recovery logging

- Log recovery admission and failure responses through `cp.error` with `partition <partitionID>:` or `workspace <wsid>:` prefixes.
- Log partition recovery start, PLog-read failure, and completion with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid=<partitionID>`.
- Log workspace recovery start, initial WLog suffix-read failure, and completion with the partition recovery attributes plus `wsid=<workspaceID>`.
- Log malformed partition and workspace checkpoints at `Error` level with the stored offset and the exact decision text `retrying from FirstOffset`; use the normal recovery context attributes on the dedicated `cp.partition_recovery.checkpoint.invalid` and `cp.workspace_recovery.checkpoint.invalid` stages.

#### VIT restart infrastructure

- The VIT shared-storage restart helper uses the same `VITConfig` for both VVM runs. It materializes config-declared logins, workspaces, documents, and subjects only during the first run; the second run launches the VVM over preserved storage without issuing fixture-provisioning commands.
- Preserve VIT lookup and authorization behavior on the second run by carrying the `appWorkspaces` and `principals` maps from the first VIT into the second VIT before common VIT initialization refreshes principal tokens. Keep these maps explicit: do not add `SecondRunConfig` or an aggregate type such as `predefinedEntities`.

### Checkpoint rationale

#### Why?

- Store the last handled offset + last IDs: wrong, IDs may be recorded incorrectly
- Store the last handled offset: wrong, we do not know last IDs
- Store both the latest ID-bearing offset seen by the projector and the last handled offset, read the tail from the last handled offset, then read the ID-bearing event separately if the tail contains no IDs: wrong, because the asynchronous projector may not yet have seen the actual latest ID-bearing event in the WLog.
  - we have to prove that IDs are really last so we need to read out the tail anyway

Problematic example:

```text
                     ID-bearing                         ID-bearing
                      ID 1001                         IDs 1002..1005           wlog end
                         |                                  |                      |
                         v                                  v                      v
WLog: ---- 1 ---- 2 ---- 3 ---- 4 ---- 5 ---- 6 ---- 7 ---- 8 ---- 9 ---- 10 ---- 11
                         ^                                  ^                      ^
                         |                                  |                      |
          latest ID-bearing offset = 3        actual latest ID-bearing    stored last offset = 10
          seen by checkpoint projector        event, not yet seen by      not correct but easily recovered by
                                              checkpoint projector        ReadWLog(10, ReadToTheEnd)

ReadWLog(10, ReadToTheEnd)  -> offset 10 exists; actual tail is 11; no ID-bearing event found
ReadWLog(3, 1)              -> offset 3 exists and is ID-bearing; validation passes
nextRecordID                -> 1002 <- WRONG
correct nextRecordID        -> 1006

The checkpoint correctly identifies event 3 as the latest ID-bearing event seen by the projector, but nothing in either read proves that the projector had reached event 7. Event 7 remains invisible to this recovery path.
```

#### Recovery flow

The safe recovery path must start at `wlogIDBearingOffset` and read through the actual tail:

```text
                     ID-bearing                         ID-bearing
                      ID 1001                         IDs 1002..1005           wlog end
                         |                                  |                      |
                         v                                  v                      v
WLog: ---- 1 ---- 2 ---- 3 ---- 4 ---- 5 ---- 6 ---- 7 ---- 8 ---- 9 ---- 10 ---- 11
                         ^                                  ^                      ^
                     checkpoint                             |                      |
                         |                            sees last IDs      sees last wlog offset
                         |----------- ReadWLog(3, ReadToTheEnd) ------------------>|


latest ID-bearing event found = 7     -> nextRecordID   = 1006
actual final event found      = 11    -> nextWLogOffset = 12
```

So:

- Store exactly one `wlogIDBearingOffset`: the latest ID-bearing event seen by the checkpoint projector.
- Require it to point to an ID-bearing event.
- Treat it only as an inclusive scan start, not as proof that the event is the latest ID-bearing event in the WLog.

The remaining tradeoff is recovery time when the projector lags or the WLog has a long non-ID-bearing suffix:

```text
                     checkpoint
                   IDs 1001..1003                                                            WLog end
                          |                                                                      |
                          v                                                                      v
WLog: ------------------ 41 ---- non-ID ---- non-ID ---- ... 1,000,000 non-ID events ... ---- 1000041
                          |<--------------- ReadWLog(41, ReadToTheEnd) ------------------------->|

events read       = 1,000,001
nextRecordID      = 1004
nextWLogOffset    = 1000042
```
#### Bad cases on recovery

What if the WLog ID-bearing offset is recorded incorrectly?

- `ReadWLog(wlogIDBearingOffset, ReadToTheEnd)`
  - no callback e.g. the `wlogIDBearingOffset` is beyond the end) -> checkpoint invalid -> `ReadWLog(FirstOffset, ReadToTheEnd)`
  - first callback offset differs from `wlogIDBearingOffset` -> checkpoint invalid -> `ReadWLog(FirstOffset, ReadToTheEnd)`
  - event in the first callback contains no new non-singleton IDs -> checkpoint invalid -> `ReadWLog(FirstOffset, ReadToTheEnd)`

Out of scope:

- Changing workspace-ID allocation or Sequence Trust Level write policy.

References (internal):

- [command/impl.go](../../../pkg/processors/command/impl.go)
- [command/provide.go](../../../pkg/processors/command/provide.go)
- [checkpoint contracts](../../../pkg/checkpoints/types.go)
- [checkpoint projector](../../../pkg/checkpoints/impl.go)
- [checkpoint registration](../../../pkg/checkpoints/provide.go)
- [storage/impl_recoverycheckpoint.go](../../../pkg/vvm/storage/impl_recoverycheckpoint.go)
- [istructs/events-types.go](../../../pkg/istructs/events-types.go)
- [vvm/types.go](../../../pkg/vvm/types.go)
- [recovery logging contract](../../specs/prod/apps/logging--td.md#command-processor)
- [AIR-5009 recovery requirements](./issue-AIR-5009.md)

References (external):

- [post-removal sequence-storage baseline in PR #4649](https://github.com/voedger/voedger/pull/4649)
- [partition-recovery performance and shared-storage scope in AIR-4959](https://untill.atlassian.net/browse/AIR-4959)

## Functional design

- [x] create: [apps/recovery-checkpoints.feature](../../specs/prod/apps/recovery-checkpoints.feature)
  - Feature specification for externally observable partition and workspace recovery from valid, absent, zero, and invalid checkpoints; empty-log defaults; non-ID-bearing WLog suffixes; workspace-recovery admission and retry behavior; and compact declarative rule flows

## Technical design

- [x] update: [apps/arch-sequences.md](../../specs/prod/apps/arch-sequences.md)
  - update: allocation and recovery relationships to include persistent recovery checkpoints and separate partition and workspace recovery managers
  - replace: full-PLog partition recovery and eager workspace reconstruction with inclusive checkpoint-tail scans and request-triggered workspace recovery
  - document: `lastPLogOffset` and `wlogIDBearingOffset` meanings, checkpoint production by `ProjectorRecoveryCheckpoint`, and why the WLog checkpoint is only a safe scan start
  - document: checkpoint validation, one fallback scan from `FirstOffset`, empty-log defaults, actual-tail recovery, and latest encountered non-singleton record-ID recovery
  - update: recovery admission, retained failure, retry, concurrency-limit, cancellation, shutdown, and stale-attempt behavior for both recovery scopes
  - update: recovery sequence diagrams, invariants, implementation links, and test evidence to match the new architecture

- [x] update: [apps/logging--td.md](../../specs/prod/apps/logging--td.md)
  - document: partition and workspace recovery admission and failure messages emitted through `cp.error`
  - document: partition and workspace recovery lifecycle stages, context attributes, and completion messages
  - document: malformed-checkpoint error stages, messages, recovery context attributes, and fallback to `FirstOffset`
  - align malformed-checkpoint messages with the exact `retrying from FirstOffset` suffix emitted by command recovery

## Construction

Gherkin scenario test placement:

- Implement the scenarios in [apps/recovery-checkpoints.feature](../../specs/prod/apps/recovery-checkpoints.feature) as VIT integration tests in [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go).
- Implement the empty-PLog and empty-WLog scenarios as mocked command-processor tests because VIT cannot create those states.
- Use a mocked command-processor test for any other scenario only when its VIT implementation proves impossible or disproportionately complicated. Document the concrete reason next to that fallback test; otherwise, do not duplicate Gherkin scenario coverage in command-processor tests.

### Tests

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - realign every VIT scenario subtest, exact Examples row, and step comment with the updated feature specification
  - for every partition-checkpoint case, assert the first request returns partition-recovery `503`, the next request returns workspace-recovery `503`, and only the request after workspace recovery succeeds
  - use `200001` through `200006` in scenario traceability comments and verify that the dynamically allocated IDs equal those values
  - assert the complete invalid-checkpoint log messages ending with `FirstOffset`, and assert their absence for absent, zero, and valid checkpoints
  - implement every deterministic non-empty-log history in VIT, including the WLog-tail boundary, all non-ID-bearing suffix kinds, a WLog without user record IDs, and checkpoints pointing to each non-ID-bearing event kind
  - leave recovery admission and failure scenarios to focused command-processor tests because pausing or failing recovery through a full VVM would require test-only lifecycle controls
  - make recovery-state observations deterministic: use dedicated untouched workspaces, disable transparent request retries for expected `503` responses, and wait for explicit VIT subscriptions or recovery lifecycle completion before issuing the next scenario request
  - verify the successful post-recovery command by reading from its returned PLog offset through the end, matching the event to the target workspace, and asserting that no later PLog event exists instead of checking only that the tail increased
  - continue reading from the successful insert's returned WLog offset through the end and assert that exactly that insert event exists there, with no later WLog event

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - replace semantic Scenario Outline disambiguators such as `zero`, `valid but outdated`, `valid`, `beyond end`, `missing event`, and event-kind abbreviations with the exact distinguishing Examples cell value, so all 20 non-empty-log Examples rows are directly discoverable from their `recovery-checkpoints: scn:` names
  - preserve the existing exact Examples header/row comments, placeholder mappings, distinct checkpoint kinds, fixture construction, and behavioral assertions; do not duplicate the existing scenario tests or expand the grouped technical tests
  - correct the seven workspace-checkpoint step comments from `Given the stored workspace checkpoint is <checkpoint>` to the feature's verbatim `And the stored workspace checkpoint is <checkpoint>`
  - explicitly retain the already exact `updates of existing records` case and its `checkpointExistingRecordSuffix` path, and the CUD-free and existing-record-update invalid-checkpoint paths at offsets 12 and 14

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - keep the grouped `TestRecoveryCheckpoints_RecordIDsRecovery` subtests as supplementary technical coverage rather than treating their generic names as feature-scenario identities
  - implement absent, zero, and invalid checkpoints over empty PLogs and WLogs with mocked logs, the only log state that VIT cannot construct
  - drive each empty-log scenario through the partition and workspace recovery managers' request-equivalent `503` stages and verify the next PLog offset, WLog offset, and record ID; direct manager tests are required because provisioning a command-capable fixture makes the logs non-empty
  - use a mocked test for another Gherkin scenario only when deterministic VIT construction is impossible or disproportionately complicated, and document that concrete reason next to the test
  - implement workspace admission limits, duplicate suppression, zero admission, and retained-failure/report/retry here with documented VIT-complexity reasons
  - prove that an excess workspace-recovery request is not queued by observing no start before a later request is submitted after a slot is released
  - verify the retained-failure/report/retry scenario through its externally observable `503`, `500`, `503`, and success sequence
  - assert complete invalid-checkpoint messages, including the `FirstOffset` fallback wording, rather than matching a proper prefix of a longer message

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - replace the `zero` aliases in the empty-PLog and empty-WLog Scenario Outline subtest names with the exact `lastPLogOffset 0` and `wlogIDBearingOffset 0` Examples values
  - preserve the existing exact Examples header/row comments, placeholder mappings, mocked recovery flow, and assertions; do not duplicate the four empty-log scenario tests

- [x] update: [checkpoints/impl_test.go](../../../pkg/checkpoints/impl_test.go)
  - verify that one projector stores `lastPLogOffset` when `PLogOffset % 100 == 0`
  - verify that each event allocating non-singleton record IDs replaces `wlogIDBearingOffset`, so it identifies the latest such event seen by the projector, while singleton, existing-record, and CUD-free events do not write a workspace checkpoint
  - verify storage-error propagation and registration of the single standard asynchronous projector

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - implement the empty-PLog and empty-WLog scenarios from [apps/recovery-checkpoints.feature](../../specs/prod/apps/recovery-checkpoints.feature) with mocked logs
  - implement another Gherkin scenario here only if its VIT implementation is impossible or disproportionately complicated, and document the concrete reason for the fallback
  - verify one inclusive PLog read from the saved `lastPLogOffset` to the end
  - verify that partition recovery does not start workspace recovery and that a subsequent request starts it from the command stage
  - verify one inclusive WLog read from the workspace's saved `wlogIDBearingOffset` to the end
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
  - verify that an invalid checkpoint over an empty log logs an error, performs at most one fallback scan, and retains the empty-log defaults

- [x] create: [command/recovery_manager_test.go](../../../pkg/processors/command/recovery_manager_test.go)
  - directly verify generic recovery-manager startup, ready lookup, duplicate suppression, retained failures, retries, admission limits, hooks, cancellation, reset, stale-completion rejection, recovered-value projection, and shutdown clearing
  - relocate stale-completion coverage from the partition-manager integration-style test

- [x] update: [command/checkpoints_test.go](../../../pkg/processors/command/checkpoints_test.go)
  - provide a thread-safe last-write-wins checkpoint test double using `lastPLogOffset` and `wlogIDBearingOffset`
  - implement partition and workspace checkpoint reads, writes, forced setup, reset, and write-order observation for command recovery tests

- [x] create: [command/recovery_test_utils_test.go](../../../pkg/processors/command/recovery_test_utils_test.go)
  - centralize deterministic test support for asynchronous partition and workspace recovery
  - provide recovery-attempt gates, injected failures, completion waits, and inclusive suffix PLog/WLog read observations
  - provide a retrying request sender for command tests whose subject is unrelated to lazy recovery

- [x] delete: [command/test_utils.go](../../../pkg/processors/command/test_utils.go)
  - replace the partition-only recovery test controls with the generic test-only utilities in `recovery_test_utils_test.go`

- [x] update: [storage/impl_recoverycheckpoint_test.go](../../../pkg/vvm/storage/impl_recoverycheckpoint_test.go)
  - verify single-field PLog and WLog JSON values named `lastPLogOffset` and `wlogIDBearingOffset`, including missing and malformed values and last-write-wins replacement
  - verify application/partition/workspace isolation through partition-key suffixes and the fixed singleton clustering column `[]byte{1}`
  - construct the test subject through `NewRecoveryCheckpointStorage` and the `IRecoveryCheckpointStorage` contract so the storage tests compile
  - compare decoded JSON numbers using the representation returned by `encoding/json` so the isolation cases pass

- [x] update: [storage/consts_test.go](../../../pkg/vvm/storage/consts_test.go)
  - preserve fixed sequence-storage prefix values

- [x] update: [actualizers/impl_helpers_test.go](../../../pkg/processors/actualizers/impl_helpers_test.go)
  - extend PLog event mocks with handling-partition and PLog-offset accessors used by the checkpoint projector

- [x] update: [istructsmem/event-types_test.go](../../../pkg/istructsmem/event-types_test.go)
  - use the exported WLog storage-key helper in the event-read failure test

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - implement every non-empty-log scenario in [apps/recovery-checkpoints.feature](../../specs/prod/apps/recovery-checkpoints.feature) as a VIT integration test
  - cover absent and zero checkpoints, earlier valid offsets, and current PLog and WLog checkpoints from the scenario examples
  - cover PLog and WLog checkpoints beyond the log end, checkpoints whose exact event is missing, and a WLog checkpoint pointing to a non-ID-bearing event
  - verify `wlogIDBearingOffset` across sequential VVM instances sharing storage
  - verify through the next successful CUD response that restart preserves each workspace's next WLog offset and record ID
  - verify through the next successful CUD response that a singleton tail advances the WLog offset but not the record ID
  - verify through the next successful CUD response that a tail event for another workspace in the same partition does not advance the first workspace's WLog offset or record ID
  - overwrite persisted recovery checkpoints between VVM runs with beyond-tail PLog and WLog offsets, zero offsets, and a WLog offset of a non-ID-bearing update event
  - record the final pre-restart insert's WLog offset and NewID, then verify through the next successful CUD response and its persisted WLog event that every overwritten-checkpoint case returns both values incremented by one
  - verify dedicated malformed-checkpoint error logs for beyond-tail and non-ID-bearing checkpoints, and no such errors for zero checkpoints
  - use one unchanged VIT config for both VVM runs and resolve the same named workspaces through `vit.WS` after restart
  - remove the second-run-only configuration and any manual reconstruction of an `AppWorkspace`

- [x] update: [sys/it/impl_test.go](../../../pkg/sys/it/impl_test.go)
  - extend the storage-preserving VIT restart test to record the owner profile WLog tail before restart
  - assert at the beginning of the second run that the tail is unchanged, proving that config-declared child workspaces were not provisioned again

- [x] update: [vit/utils.go](../../../pkg/vit/utils.go)
  - retain the shared-storage two-VVM lifecycle helper and its hooks for inspecting stable storage after each VVM stops
  - use the same `VITConfig` for both VVM runs
  - after the first-run hook, retain `appWorkspaces` and `principals` as two explicit maps
  - launch the second VIT with config-entity provisioning disabled, assign both retained maps, and then run common VIT initialization so principal tokens are refreshed
  - do not add a second-run configuration hook or an aggregate type for the retained maps

- [x] update: [vit/impl.go](../../../pkg/vit/impl.go)
  - extract common VIT initialization performed after VVM launch so normal creation and storage-preserving restart apply options, validate the email captor, advance test time, refresh principal tokens, and capture the initial goroutine count consistently
  - replace the ambiguous launch-only switch with an explicit `createEntities` decision
  - when `createEntities` is false, still apply the VVM config, register workspace templates, and launch services, but skip sign-up, workspace initialization, configured document and subject creation, and post-initialization fixture actions
  - keep `appWorkspaces` and `principals` as explicit inputs on the restart path rather than defining a generic or vaguely named holder type

### Checkpoint contracts and storage

- [x] update: [istructs/events-types.go](../../../pkg/istructs/events-types.go)
  - expose handling partition and PLog offset on persisted PLog events for checkpoint projection
  - keep `IIDGenerator` free of checkpoint-specific record-ID accessors

- [x] update: [istructsmem/utils.go](../../../pkg/istructsmem/utils.go)
  - export the PLog and WLog storage-key helpers used to construct exact missing-event checkpoint cases

- [x] update: [istructsmem/impl.go](../../../pkg/istructsmem/impl.go)
  - use the exported PLog and WLog storage-key helpers for event persistence and reads

- [x] update: [storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - retain the sequence-storage prefix values and document partition ID and WSID as partition-key suffixes rather than clustering columns
  - replace the `lastWLogOffsetWithNewRecordIDs` JSON field constant with `wlogIDBearingOffset`

- [x] update: [storage/impl_recoverycheckpoint.go](../../../pkg/vvm/storage/impl_recoverycheckpoint.go)
  - append partition ID or WSID to the corresponding application partition key and use `[]byte{1}` as the clustering column
  - encode only `lastPLogOffset` for PLog checkpoints and only `wlogIDBearingOffset` for WLog checkpoints
  - report missing values as absent, reject malformed values, and overwrite unconditionally

- [x] update: [storage/provide.go](../../../pkg/vvm/storage/provide.go)
  - construct the recovery-checkpoint adapter over shared system-VVM storage

- [x] create: [checkpoints/consts.go](../../../pkg/checkpoints/consts.go)
  - define the 100-event partition-checkpoint interval and `ProjectorRecoveryCheckpoint` QName

- [x] update: [checkpoints/types.go](../../../pkg/checkpoints/types.go)
  - define a PLog checkpoint containing only `lastPLogOffset` and a WLog checkpoint containing only `wlogIDBearingOffset`
  - name the Go workspace-checkpoint field `WLogIDBearingOffset`; its documentation must state that it identifies the latest ID-bearing event seen by the projector but need not identify the latest ID-bearing event in the WLog
  - define the partition and workspace checkpoint storage contract

- [x] update: [checkpoints/impl.go](../../../pkg/checkpoints/impl.go)
  - implement one recovery-checkpoint projector for both checkpoint types
  - replace `wlogIDBearingOffset` whenever the projector sees an ID-bearing event, making it the latest such offset seen by the projector without  recovery depend on the projector having reached the WLog tail
  - persist the PLog offset when `PLogOffset % 100 == 0`

- [x] create: [checkpoints/provide.go](../../../pkg/checkpoints/provide.go)
  - register the single built-in asynchronous recovery-checkpoint projector with stateless resources

- [x] update: [parser/impl_analyse.go](../../../pkg/parser/impl_analyse.go)
  - resolve the built-in generic Command trigger used by the recovery-checkpoint projector

- [x] update: [parser/impl_build.go](../../../pkg/parser/impl_build.go)
  - map the generic Command trigger to command events

- [x] update: [sys/sys.vsql](../../../pkg/sys/sys.vsql)
  - replace both built-in recovery-checkpoint declarations with one projector for command, CUD, and ODoc events

- [x] update: [pkg/sys/sys.vsql](../../../pkg/sys/it/testdata/apps/test2.app1/image/pkg/sys/sys.vsql)
  - mirror the single built-in recovery-checkpoint projector declaration in the generated integration-test schema

- [x] update: [sys/sysprovide/provide.go](../../../pkg/sys/sysprovide/provide.go)
  - register the single checkpoint projector with stateless resources

### Command recovery

- [x] update: [command/consts.go](../../../pkg/processors/command/consts.go)
  - define shared errors for recovery-in-progress, concurrency-limit, and retained-failure outcomes

- [x] create: [command/recovery_manager.go](../../../pkg/processors/command/recovery_manager.go)
  - implement the generic keyed recovery lifecycle shared by partitions and workspaces
  - reserve optional concurrency slots before creating attempts, retain failures for one reporting request, and publish successful recovered values
  - reject stale completions after reset and join all workers before clearing state during shutdown

- [x] update: [command/types.go](../../../pkg/processors/command/types.go)
  - define generic recovery values, attempts, hooks, and manager state for absent, recovering, failed, and ready units
  - define partition and workspace keys and manager wrappers over the shared generic recovery manager
  - retain application identity and structures in recovered partitions for asynchronous WLog recovery

- [x] update: [command/impl.go](../../../pkg/processors/command/impl.go)
  - recover the partition with one inclusive PLog suffix scan and use only its final event
  - publish the recovered partition without starting workspace recovery from the partition-recovery goroutine
  - start workspace recovery on demand from the command pipeline's workspace stage
  - recover the WLog end offset and latest non-singleton record ID with one inclusive scan from `wlogIDBearingOffset` through the actual tail
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
  - validate that a stored checkpoint identifies the exact first event returned by its inclusive suffix scan, and that a WLog checkpoint event is ID-bearing
  - abort an invalid suffix scan and retry once from `FirstOffset`, using the full fallback scan instead of a separate first-event probe
  - recover empty fallback scans with `FirstOffset` and `FirstUserRecordID` defaults and never loop beyond two total scans
  - log malformed checkpoints through the dedicated partition or workspace error stage with existing recovery context attributes
  - leave checkpoint repair to the asynchronous projector rather than writing checkpoint storage during recovery
  - propagate PLog write errors so the command pipeline can schedule partition recovery

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
