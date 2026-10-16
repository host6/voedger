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

Partition and workspace sequence state must survive a VVM restart without rebuilding every counter from the complete log on demand. Existing deployments can already contain PLog and WLog history while having no recovery checkpoints, so VVM startup must backfill the initial checkpoint set before normal request-driven recovery begins.

## What

In the production application-processing context:

- Store only the last handled PLog offset in the partition checkpoint.
- Store one WLog ID-bearing offset: the offset of the most recent ID-bearing event found by initial recovery or observed by the checkpoint projector. Do not persist the last WLog offset or record IDs themselves.
- During VVM bootstrap, ensure that every declared application partition has a partition checkpoint. For each partition whose checkpoint is absent, scan its complete PLog, compute workspace checkpoints for every WSID encountered, persist those workspace checkpoints, and persist the partition checkpoint last.
- Represent a successfully initialized empty PLog with a present partition checkpoint whose `lastPLogOffset` is zero. Represent an encountered workspace with no ID-bearing event with a present workspace checkpoint whose `wlogIDBearingOffset` is zero.
- Recover a partition by reading the PLog from its saved handled offset through the end, using the final event to set the next PLog offset, and reapplying that final event.
- After partition recovery, let the requesting command continue to the workspace stage, which starts workspace recovery on demand.
- Recover a workspace by reading its WLog inclusively from the WLog ID-bearing offset through the end. Use the actual final event to set the next WLog offset and the latest ID-bearing event encountered in the complete suffix to restore record-ID allocation.
- Treat a stored checkpoint as invalid when its inclusive suffix scan invokes no callback, its first callback has a different offset, or a workspace checkpoint event did not allocate a non-singleton record ID. Log an error and recover from `FirstOffset` instead of accepting counters derived from an empty or incomplete suffix.
- If a PLog is empty, start its next offset at `FirstOffset`. If a WLog is empty, start its next offset at `FirstOffset` and its next record ID at `FirstUserRecordID`.
- Return `503 Service Unavailable` while recovery of the requested partition or workspace is already running or if the partition already has `NumWSRecoverers` workspace recoveries running. Log a corresponding message for each case.
- Emit partition recovery lifecycle logs with `vapp=sys/voedger`, `extension=sys._Recovery`, and `partid`; emit workspace recovery lifecycle logs with those attributes plus `wsid`.
- Keep the default workspace-recovery limit at four per partition.

## How

### Key concepts

- **Recovery checkpoint** (or **checkpoint**): a persisted set of log offsets used as inclusive recovery-scan starts. A checkpoint accelerates recovery but is not an authoritative copy of recovered sequence state.
- **Partition checkpoint**: `lastPLogOffset`, the latest PLog offset selected by initial recovery or subsequently stored by the checkpoint projector for a partition. A present zero value is an initialization sentinel marking an initialized empty PLog; it is not an event offset to validate.
- **Workspace checkpoint**: `wlogIDBearingOffset`, the offset of the latest ID-bearing WLog event found by initial recovery or subsequently seen by the checkpoint projector for a workspace. A present zero value is an initialization sentinel marking an encountered workspace for which no ID-bearing event was found; it is not an event offset to validate.
- **ID-bearing event**: a WLog event that allocates at least one new non-singleton record ID.
- **Initial recovery**: bootstrap-time reconstruction of missing recovery checkpoints from complete PLogs before an application's partitions and asynchronous projectors are started. It initializes checkpoint storage only; it does not restore or publish runtime partition or workspace sequence state.
- **Known partition**: every partition ID in the application deployment descriptor's contiguous declared range `0..NumPartitions-1`, whether or not that partition has a PLog event or allocated storage yet.
- **Initial-recovery worker limit**: `runtime.GOMAXPROCS(0)`, sampled once when an application's initial recovery starts. It is the Go runtime's effective available-processor limit and bounds the number of that application's missing partitions whose checkpoints may be computed concurrently.
- **Initial-recovery completion marker**: the presence of a partition checkpoint. Initial recovery writes all workspace checkpoints derived from that partition before writing its partition checkpoint, so an absent partition checkpoint means that the partition must be scanned again.
- **Inclusive suffix scan**: a log read that starts with the event identified by a nonzero checkpoint and continues through the actual log tail. The first returned event validates the checkpoint; the complete suffix supplies the recovered values. An absent or zero checkpoint instead selects a direct full scan from `FirstOffset`.
- **Checkpoint invalid**: a nonzero checkpoint exists but any of:
  - `Read*Log(storedOffset, ReadToTheEnd)` produces no callback (e.g. beyond the end)
  - offset of the event on the first callback of `Read*Log(storedOffset, ReadToTheEnd)` is not equal to `storedOffset` (e.g. points to an unoccupied offset in the middle of the log)
  - for `Workspace checkpoint`: event on the first callback of `Read*Log(storedOffset, ReadToTheEnd)` has no new IDs (e.g. `storedOffset` incorrectly recorded)
- **Checkpoint fallback**: one inclusive scan from `FirstOffset` when a stored checkpoint is invalid. Recovery does not rewrite the checkpoint.
- **Partition recovery**: Command-processor-scoped recovery that restores the next PLog offset and republishes the application partition.
- **Workspace recovery**: Command-prcoessor-scoped recovery that restores one workspace's next WLog offset and record ID after its partition has recovered.

### Decisions

- Store only `lastPLogOffset` per partition and `wlogIDBearingOffset` per workspace. Checkpoints are inclusive scan starts, not authoritative sequence state; do not store the last WLog offset or record-ID high-water marks.
- `ProjectorRecoveryCheckpoint` advances the partition checkpoint every 100 PLog events and replaces the workspace checkpoint on each ID-bearing event.
- Bootstrap orchestrates initial recovery through `checkpoints.IInitialRecovery`; `pkg/checkpoints` owns event scanning and checkpoint calculation.
- A present partition checkpoint skips initial recovery. For each missing checkpoint in `0..NumPartitions-1`, scan the complete PLog and retain its end plus the latest ID-bearing WLog offset, or zero, for every encountered WSID.
- Run partition scans concurrently with a per-application limit sampled from `runtime.GOMAXPROCS(0)`. Persist all computed workspace checkpoints before the partition checkpoint.
- A partition-scoped scan or storage failure is logged, leaves its partition checkpoint absent, and does not cancel sibling scans, fail bootstrap, or degrade server readiness. Only pre-scheduling application setup failure or bootstrap cancellation is fatal.
- Initial recovery initializes checkpoint storage only. Runtime partition and workspace states remain request-recovered.
- An absent or zero checkpoint starts one scan at `FirstOffset` without validation or invalid-checkpoint logging. A nonzero checkpoint must identify the first returned event, and a workspace checkpoint must identify an ID-bearing event; otherwise recovery logs the error and performs one fallback scan from `FirstOffset`.
- Partition recovery restores the next PLog offset from the actual tail and reapplies the final event. A later request starts workspace recovery, which restores the next WLog offset and next non-singleton record ID from the actual WLog tail.
- Recovery admission remains `503` while work runs. A retained failure is returned as `500` while one retry starts; an in-progress retry returns `503`.
- Workspace recovery remains bounded by `NumWSRecoverers`, deduplicated by WSID, tied to the service context, and joined on shutdown.
- Recovery logs identify the application, partition, workspace when applicable, failed operation, and underlying error.
- `sequences.feature` covers successful user-visible issuance; `sequences--td.md` keeps issuance internals and failure-induced gaps. All checkpoint and recovery behavior belongs in `recovery.feature` and `recovery--td.md`.

Notes:

- Workspace chekpoints existance for each WSID is not checked on Intial recovery
- The last event in the plog is not applied on the Inital Recovery. It is done on the first command to the partition
- Query does not initiate partition or workspace recovery. Only a command does.

### Checkpoint rationale

Store the offset of the last-seen wlog ID-bearing event

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

What if the PLog offset is recorded incorrectly?

- `lastPLogOffset` is absent or zero -> this is not an invalid checkpoint -> `ReadPLog(FirstOffset, ReadToTheEnd)` once, with no checkpoint-error log
- `ReadPLog(lastPLogOffset, ReadToTheEnd)`
  - no callback -> checkpoint invalid -> `ReadPLog(FirstOffset, ReadToTheEnd)`
  - first callback offset differs from `lastPLogOffset` -> checkpoint invalid -> `ReadPLog(FirstOffset, ReadToTheEnd)`

What if the WLog ID-bearing offset is recorded incorrectly?

- `wlogIDBearingOffset` is absent or zero -> this is not an invalid checkpoint -> `ReadWLog(FirstOffset, ReadToTheEnd)` once, with no checkpoint-error log
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

- [x] create: [apps/sequences.feature](../../specs/prod/apps/sequences.feature)
  - Feature specification for successful, externally observable allocation of PLog offsets, WLog offsets, ordinary record IDs, singleton IDs, and workspace IDs; exclude failed-command behavior, implementation-level persistence policy, restart, checkpoint, and recovery behavior

- [x] rename: [apps/recovery-checkpoints.feature](../../specs/prod/apps/recovery-checkpoints.feature) -> [apps/recovery.feature](../../specs/prod/apps/recovery.feature)
  - rename the feature to partition and workspace recovery
  - add a rule for bootstrap-time initial recovery when a declared partition has no checkpoint
  - define known partitions as the complete declared `0..NumPartitions-1` range and include empty partitions in initialization
  - specify that every missing partition checkpoint is computed from the complete PLog and persisted after the workspace checkpoints derived from that partition
  - specify that every WSID encountered in the scan receives a workspace checkpoint, using zero when no ID-bearing event was found
  - specify empty-PLog initialization as a present zero-valued partition checkpoint
  - specify partition-isolated initial-recovery failure: other partition attempts finish, bootstrap continues, the failed partition remains without a completion marker, and its first command enters the existing `503`, retained-error `500`/single-retry, and retry-in-progress `503` lifecycle when the underlying error persists
  - specify that the server becomes ready even when every partition initialization fails, so callers can observe persistent recovery errors through their requests rather than only through failed-startup logs
  - specify that successful initial recovery initializes checkpoint storage only and that the first later request still enters normal partition recovery followed by lazy workspace recovery
  - retain request-driven partition/workspace recovery, checkpoint validation, fallback, empty-log defaults, admission limits, and retry behavior under recovery-focused rules
  - remove normal sequence-allocation behavior; it belongs in `sequences.feature`

## Technical design

- [x] delete: [apps/arch-sequences.md](../../specs/prod/apps/arch-sequences.md)
  - replace its current-behavior content with the focused sequence-allocation and recovery feature technical designs below; retain historical material only through the existing historical references

- [x] create: [apps/sequences--td.md](../../specs/prod/apps/sequences--td.md)
  - Feature Technical Design whose Key concepts define sequence state, next values, partition/workspace/application allocation scopes, ordinary versus singleton record IDs, and successful-write advancement; cover the components, persistence boundaries, Sequence Trust Level policy, and failure-induced gaps needed to explain normal PLog-offset, WLog-offset, record-ID, and workspace-ID issuance, cite existing technical-test evidence for failure edges instead of adding those cases to `sequences.feature`, and explicitly exclude checkpointing and recovery

- [x] create: [apps/recovery--td.md](../../specs/prod/apps/recovery--td.md)
  - Feature Technical Design whose Key concepts define recovery checkpoints, partition and workspace checkpoints, their zero-value initialization sentinels, ID-bearing events, initial recovery, the initial-recovery completion marker, inclusive suffix scans, checkpoint fallback, partition recovery, and workspace recovery; cover bootstrap orchestration, checkpoint production and storage, request-driven recovery, admission and retry lifecycles, failure ordering, logging, and shutdown

- [x] update: [apps/arch.md](../../specs/prod/apps/arch.md)
  - remove `arch-sequences.md` as the canonical Sequences subsystem architecture
  - link `sequences.feature` with `sequences--td.md` and `recovery.feature` with `recovery--td.md` as the focused feature designs

- [x] update: [apps/arch2-sequences.md](../../specs/prod/apps/arch2-sequences.md)
  - replace the link to the deleted canonical sequence architecture with links to the new sequence-allocation and recovery specifications while retaining this file as historical context only

- [x] update: [apps/arch-deployment.md](../../specs/prod/apps/arch-deployment.md)
  - document that bootstrap invokes the checkpoint initial-recovery component after app registration and before partition deployment
  - document bounded per-application partition initialization, waiting for every scheduled attempt, and continuation into partition deployment despite logged partition-scoped failures
  - define readiness as independent of partition-scoped initial-recovery results, including the all-partitions-failed case
  - distinguish failures before partition scheduling and bootstrap cancellation, which still stop bootstrap, from every failure inside a scheduled partition attempt, which does not

- [x] update: [apps/logging--td.md](../../specs/prod/apps/logging--td.md)
  - document: partition and workspace recovery admission and failure messages emitted through `cp.error`
  - document: partition and workspace recovery lifecycle stages, context attributes, and completion messages
  - document: malformed-checkpoint error stages, messages, recovery context attributes, and fallback to `FirstOffset`
  - align malformed-checkpoint messages with the exact `retrying from FirstOffset` suffix emitted by command recovery

- [x] update: [apps/logging--td.md](../../specs/prod/apps/logging--td.md)
  - document initial-recovery start, per-partition completion, failure, and app completion stages under the bootstrap/recovery context
  - require app QName and partition ID on per-partition initial-recovery logs, distinguish scan failures from checkpoint-storage failures, and state that these errors are partition-isolated rather than bootstrap-fatal

## Construction

Gherkin scenario test placement:

- Implement the normal allocation scenarios in [apps/sequences.feature](../../specs/prod/apps/sequences.feature) as VIT integration tests in [sys/it/impl_sequences_test.go](../../../pkg/sys/it/impl_sequences_test.go); reuse or move exact existing coverage instead of duplicating it.
- Implement the scenarios in [apps/recovery.feature](../../specs/prod/apps/recovery.feature) as VIT integration tests in [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go).
- Implement the empty-PLog and empty-WLog scenarios as mocked command-processor tests because VIT cannot create those states.
- Use a mocked command-processor test for any other scenario only when its VIT implementation proves impossible or disproportionately complicated. Document the concrete reason next to that fallback test; otherwise, do not duplicate Gherkin scenario coverage in command-processor tests.

### Tests

- [x] create: [sys/it/impl_sequences_test.go](../../../pkg/sys/it/impl_sequences_test.go)
  - VIT scenario tests for `sequences.feature`
  - verify successful partition-scoped PLog-offset issuance, workspace-scoped WLog-offset and record-ID issuance, singleton-ID behavior, and persistent application-scoped WSID allocation; keep failed-command and recovery cases out of these feature scenarios
  - move or rename exact existing tests where possible; use `sequences: scn:` names matching the feature scenarios, verbatim step comments at the code that realizes each step, and exact Examples header/row comments and placeholder mappings for every Scenario Outline row

- [x] create: [checkpoints/initial_recovery_test.go](../../../pkg/checkpoints/initial_recovery_test.go)
  - unit tests for bootstrap-time checkpoint reconstruction over multiple declared partitions and workspaces
  - cover every partition in the declared `0..NumPartitions-1` range, present and absent checkpoints, empty PLogs with no allocated storage, workspaces with and without ID-bearing events, and selection of the latest ID-bearing WLog offset
  - verify the concurrency bound, continuation of other partitions after one scan or storage failure, waiting for all attempts, workspace-before-partition persistence order, retry on the next bootstrap, no writes for a completed partition, context cancellation, and absence of any runtime recovery/publication side effect
  - capture and assert one operation-specific error log for each failed partition
  - use a private test constructor with an explicit worker limit to prove the bound deterministically without changing process-wide `GOMAXPROCS`; verify that the public constructor samples `runtime.GOMAXPROCS(0)` for each application initialization

- [x] update: [sys/it/impl_bootstrap_test.go](../../../pkg/sys/it/impl_bootstrap_test.go)
  - use an injected test initializer to verify the technical orchestration contract: bootstrap invokes initial recovery for every registered app before deploying its partitions
  - verify that one or all logged partition-scoped failures do not stop partition deployment or server readiness, while a pre-scheduling application setup error or bootstrap cancellation still stops deployment; leave the feature scenarios and checkpoint-value assertions in `impl_recovery_test.go`

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - rename feature traceability from `recovery-checkpoints: scn:` to `recovery: scn:` and align step comments with `recovery.feature`
  - replace request-time absent-partition-checkpoint scenarios with bootstrap initial-recovery scenarios
  - retain direct technical coverage for defensive request-time handling of a checkpoint removed after bootstrap
  - simulate a pre-checkpoint deployment by creating PLog histories in shared storage and removing their partition completion markers before the next VVM launch
  - verify that bootstrap initializes every missing declared partition, persists the actual PLog tail, creates a workspace checkpoint for every WSID encountered, and stores zero for empty/non-ID-bearing histories
  - inject one partition-scoped initial-recovery failure and verify that other partitions receive completion markers, bootstrap continues, the failed partition remains unmarked, and its command observes `503`, retained-error `500` with one retry, and `503` while the retry runs if the failure persists
  - inject failures for every partition and verify that the server still reports ready and accepts commands, allowing each affected partition's request-driven recovery to surface its own persistent error
  - verify across shared-storage VVM runs that initial recovery creates checkpoints once and later bootstrap runs preserve completed partition markers
  - verify that checkpoint initialization does not eagerly recover runtime state: the first command still receives partition-recovery `503`, the next reaches lazy workspace recovery, and only the later retry succeeds
  - give every feature scenario an exact `recovery: scn:` subtest name, place each verbatim step comment immediately above the code that realizes it, and preserve exact Examples header/row comments and placeholder mappings

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - rename retained feature scenario tests to the `recovery: scn:` prefix
  - keep request-time missing-partition-checkpoint behavior as a technical fallback test rather than an initial-recovery feature scenario
  - verify that absent and zero-valued partition and workspace checkpoints each cause exactly one read from `FirstOffset`, no validation attempt, no fallback read, and no invalid-checkpoint log

- [x] update: [checkpoints/impl_test.go](../../../pkg/checkpoints/impl_test.go)
  - exercise the shared ID-bearing-event predicate used by both initial recovery and the asynchronous checkpoint projector

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
  - replace semantic Scenario Outline disambiguators such as `zero`, `valid but outdated`, `valid`, `beyond end`, `missing event`, and event-kind abbreviations with the exact distinguishing Examples cell value, so all 20 non-empty-log Examples rows are directly discoverable from their final `recovery: scn:` names
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
  - implement the empty-PLog and empty-WLog scenarios from the renamed [apps/recovery.feature](../../specs/prod/apps/recovery.feature) with mocked logs
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
  - implement every non-empty-log scenario in the renamed [apps/recovery.feature](../../specs/prod/apps/recovery.feature) as a VIT integration test
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

### Initial recovery

- [ ] create: [checkpoints/initial_recovery.go](../../../pkg/checkpoints/initial_recovery.go)
  - bootstrap-time `IInitialRecovery` implementation backed by the app-structures provider and recovery-checkpoint storage
  - `NewInitialRecovery` constructor and `Ensure(ctx, appQName, numPartitions)` operation that enumerates `0..numPartitions-1`, skips partitions with present completion markers, and scans each missing partition from `FirstOffset` through the PLog tail
  - per-partition accumulation of the final PLog offset and latest ID-bearing WLog offset for every encountered WSID, including zero-valued checkpoints for empty or non-ID-bearing histories
  - production worker limit sampled from `runtime.GOMAXPROCS(0)` at the start of each `Ensure` call, plus a private constructor seam for deterministic lower-limit tests
  - bounded worker scheduling that waits for every partition attempt and does not cancel siblings after a partition-scoped failure
  - ordered persistence of all computed workspace checkpoints before the partition completion marker, with partition-scoped read/storage errors logged and suppressed and only application-level setup or context errors returned to bootstrap

- [ ] update: [checkpoints/types.go](../../../pkg/checkpoints/types.go)
  - define `IInitialRecovery` with `Ensure(ctx, appQName, numPartitions) error` for injection into bootstrap
  - document checkpoint presence, rather than a nonzero offset, as the initial-recovery completion marker
  - document zero-valued partition and workspace checkpoints produced for empty and non-ID-bearing histories and identify both zero values as non-validating full-scan sentinels

- [ ] update: [checkpoints/impl.go](../../../pkg/checkpoints/impl.go)
  - extract one ID-bearing-event predicate shared by initial recovery and `ProjectorRecoveryCheckpoint`
  - keep the asynchronous projector as the normal post-bootstrap checkpoint producer

- [ ] update: [btstrp/impl.go](../../../pkg/btstrp/impl.go)
  - accept the service context and injected initial-recovery contract
  - invoke initial recovery after each app is registered with app structures and before `DeployAppPartitions` starts its partition workers
  - apply the same ordering to the cluster app, built-in apps, and sidecar apps
  - continue into partition deployment and successful readiness after the initializer has logged any number of partition-scoped failures; stop bootstrap only when the initializer returns a pre-scheduling application setup or context error

- [ ] update: [vvm/provide.go](../../../pkg/vvm/provide.go)
  - construct `checkpoints.IInitialRecovery` from the app-structures provider and recovery-checkpoint storage
  - inject it and the bootstrap operator context into `btstrp.Bootstrap`

- [ ] regenerate: [vvm/wire_gen.go](../../../pkg/vvm/wire_gen.go)
  - reflect construction and bootstrap injection of the checkpoint initial-recovery component

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
  - name the Go workspace-checkpoint field `WLogIDBearingOffset`; its documentation must state that it identifies the latest ID-bearing event found by its producer but need not identify the latest ID-bearing event in the WLog
  - define the partition and workspace checkpoint storage contract

- [x] update: [checkpoints/impl.go](../../../pkg/checkpoints/impl.go)
  - implement one recovery-checkpoint projector for both checkpoint types
  - replace `wlogIDBearingOffset` whenever the projector sees an ID-bearing event, making it the latest such offset seen by the projector without  recovery depend on the projector having reached the WLog tail
  - persist the PLog offset when `PLogOffset % 100 == 0`

- [x] create: [checkpoints/provide.go](../../../pkg/checkpoints/provide.go)
  - register the single built-in asynchronous recovery-checkpoint projector with stateless resources

- [x] update: [sys/sys.vsql](../../../pkg/sys/sys.vsql)
  - replace both built-in recovery-checkpoint declarations with one projector using `AFTER EXECUTE ON ALL COMMANDS` together with the CUD and ODoc triggers

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
