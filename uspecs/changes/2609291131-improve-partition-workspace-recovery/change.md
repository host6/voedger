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

The application-processing recovery path needs durable checkpoints for partition and workspace sequence state; reconstructing counters from event history makes recovery work grow with that history. Demand-driven, bounded recovery improves responsiveness while preserving sequence continuity.

## What

In the production application-processing context:

- Partition recovery restores persisted partition progress and workspace sequence state into memory, reapplies the last event, and saves workspace state before partition progress.
- Recovery checkpoints stay current during normal processing: partition progress is recorded periodically, and workspace sequence state is recorded for every operation.
- A command for an unrecovered partition starts its recovery; a command for an unrecovered workspace in a recovered partition starts workspace recovery and receives a retryable service-unavailable response until recovery completes.
- Workspace recovery concurrency is bounded per partition and defaults to four workers.

## Constraints

- Do not modify any documentation changed by [PR #4651](https://github.com/voedger/voedger/pull/4651) in this pull request. Update that documentation in a separate follow-up pull request after all related pull requests are approved.
- Design and implement against the expected result of [PR #4649](https://github.com/voedger/voedger/pull/4649): the currently unused `isequencer` implementation is removed, while the sequence trust-level constants retained in `pkg/isequencer/consts.go` remain available.

## How

Decisions:

- Extend command-processor recovery into two readiness levels: retain one service-scoped recovery lifecycle per application partition, and add deduplicated on-demand workspace recovery scheduled through a bounded worker pool owned by that partition. Normalize a zero worker limit to the default so zero-valued configurations remain usable.
- Store extensible JSON checkpoints in shared system-VVM storage: a next-PLog-offset snapshot keyed by application and partition, and a next-WLog-offset plus next-record-ID snapshot keyed by application and workspace. Reuse the retained sequence-storage key prefixes with a dedicated nonzero four-byte clustering key so legacy binary cells remain untouched and distinguishable while the partition key can host additional cell types later.
- Capture immutable checkpoint snapshots from command-processor memory only after the command's PLog, records, synchronous projections, and WLog have succeeded. Expose the record-ID generator's last allocated or synchronized value for this purpose instead of reconstructing outgoing snapshots from events or reviving the removed generic sequencer.
- Use two built-in asynchronous checkpoint projectors owned by the command service rather than schema-defined async actualizers: persist workspace snapshots for every successful operation, and persist partition progress after each 100 covered events or one minute, whichever comes first.
- Treat the partition checkpoint as a durability barrier: never advance it past an event until that event's workspace snapshot is durable. Use conditional monotonic updates that merge next offsets and record IDs by maximum so delayed work or overlapping VVM handoff cannot regress shared state.
- Start partition recovery from the persisted next PLog offset; when it is beyond the first offset, read the immediately preceding event as the initial reapplication candidate, then scan only the uncheckpointed tail and replace the candidate with any newer tail event. Merge tail values with workspace snapshots, reapply the resulting last event, and then save affected workspace snapshots before advancing partition progress. When the partition checkpoint is missing or zero, perform one full-PLog bootstrap scan and seed both checkpoint levels.
- Publish workspaces reconstructed by the partition tail immediately; recover all other workspaces lazily from their snapshots. Workspace attempts follow the existing recovery lifecycle for deduplication, service-lifetime cancellation, retained failures, and on-demand retry.
- Keep checkpoint failures off the successful command response path: retry and report them operationally without advancing the partition barrier. On orderly shutdown, stop accepting snapshots, attempt one final ordered flush, cancel any remaining retries through the service context, and join checkpoint and recovery workers before releasing command-service resources.
- Verify crash boundaries, stale-writer ordering, checkpoint bootstrap, bounded workspace concurrency, and shared-storage recovery across multiple VVM instances with deterministic gates and injected storage failures.

Assumptions:

- Command routing maintains at most one active command writer for an application partition; monotonic conditional checkpoint updates protect state during handoff or transient overlap but do not provide distributed command serialization.

Out of scope:

- Changing workspace-ID allocation or Sequence Trust Level write policy.

References (internal):

- [active partition recovery and counter reconstruction](../../../pkg/processors/command/impl.go)
- [command persistence and service-lifetime boundaries](../../../pkg/processors/command/provide.go)
- [recovery concurrency, failure, and shutdown behavior](../../../pkg/processors/command/impl_test.go)
- [event-based async actualizer lifecycle](../../../pkg/processors/actualizers/async.go)
- [shared system-storage conditional operations](../../../pkg/vvm/storage/interface.go)
- [retained sequence-storage key prefixes](../../../pkg/vvm/storage/consts.go)
- [in-memory record-ID allocation](../../../pkg/istructsmem/idgenerator.go)
- [VVM configuration boundary](../../../pkg/vvm/types.go)
- [AIR-5009 recovery and checkpoint requirements](./issue-AIR-5009.md)

References (external):

- [post-removal sequence-storage baseline in PR #4649](https://github.com/voedger/voedger/pull/4649)
- [partition-recovery performance and shared-storage scope in AIR-4959](https://untill.atlassian.net/browse/AIR-4959)

## Construction

### Tests

- [x] create: [command/checkpoints_test.go](../../../pkg/processors/command/checkpoints_test.go)
  - exercise the asynchronous workspace and partition checkpoint workers with deterministic time, storage failures, and queue gates
  - verify per-operation workspace writes, the 100-event/one-minute partition cadence, workspace-before-partition ordering, monotonic stale-write handling, retries, and the final shutdown flush
  - cover stale snapshots and overlapping writers without allowing offsets or record IDs to regress

- [x] update: [command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - update: recovery coverage for a full bootstrap scan when the partition checkpoint is missing or zero and a tail-only scan when it contains a usable next offset
  - add: coverage that an up-to-date checkpoint with an empty tail still reads and reapplies the preceding last event
  - add: workspace recovery coverage for first-request and in-progress `503` responses, one attempt per workspace, the configured per-partition concurrency bound, retained failures, retries, and service cancellation
  - add: coverage that a zero workspace-worker setting uses the default instead of blocking recovery
  - preserve: last-event reapplication plus PLog offset, WLog offset, and record-ID continuity across partition and workspace recovery
  - add: coverage that affected workspace snapshots are saved before recovered partition progress is published
  - add: coverage that PLog, record-application, synchronous-projector, and WLog failures never enqueue a workspace checkpoint or advance the partition barrier

- [x] create: [storage/impl_recoverycheckpoint_test.go](../../../pkg/vvm/storage/impl_recoverycheckpoint_test.go)
  - exercise missing, valid, malformed, and concurrent partition/workspace checkpoint reads and writes
  - verify the JSON payloads and application/partition/workspace key isolation
  - prove monotonic conditional updates and coexistence with legacy binary cells under the retained key prefixes

- [x] update: [istructsmem/idgenerator_test.go](../../../pkg/istructsmem/idgenerator_test.go)
  - add: coverage for reading the last record ID after initialization, allocation, and synchronization updates through the existing generator contract

- [x] update: [storage/consts_test.go](../../../pkg/vvm/storage/consts_test.go)
  - preserve: fixed sequence-storage prefix values and surrounding prefix order while the prefixes become active checkpoint namespaces

- [x] update: [sys/it/impl_recovery_test.go](../../../pkg/sys/it/impl_recovery_test.go)
  - update: the shared-storage restart scenario to exercise checkpoint bootstrap followed by recovery on another VVM instance
  - add: assertions that offsets and record IDs remain continuous across VVM handoff and stale checkpoint writes cannot overwrite newer shared state

### Checkpoint contracts and storage

- [x] update: [istructs/events-types.go](../../../pkg/istructs/events-types.go)
  - add: a non-mutating last-record-ID accessor to the existing ID-generator contract for recovery-state capture

- [x] update: [istructsmem/idgenerator.go](../../../pkg/istructsmem/idgenerator.go)
  - add: a non-mutating last-record-ID accessor on the existing generator implementation
  - preserve: existing allocation, synchronization, and hook behavior

- [x] update: [storage/consts.go](../../../pkg/vvm/storage/consts.go)
  - retain: the existing numeric sequence-storage prefixes from PR #4649 and define their active checkpoint/legacy-cell roles without renumbering later prefixes

- [x] create: [storage/impl_recoverycheckpoint.go](../../../pkg/vvm/storage/impl_recoverycheckpoint.go)
  - system-VVM storage adapter for partition and workspace recovery checkpoints using the retained prefixes and a dedicated nonzero four-byte clustering key
  - extensible JSON values containing the next PLog offset or the next WLog offset and record ID
  - monotonic insert/compare-and-swap loops that merge each next value by maximum and preserve legacy binary cells
  - report missing values as absent for bootstrap recovery, but return malformed JSON as an operational recovery error

- [x] update: [storage/provide.go](../../../pkg/vvm/storage/provide.go)
  - add: construction of the recovery-checkpoint storage adapter over the shared system-VVM storage

### Command recovery and checkpointing

- [x] create: [command/checkpoints.go](../../../pkg/processors/command/checkpoints.go)
  - recovery-checkpoint storage contract and immutable partition/workspace snapshot types
  - service-scoped workspace and partition checkpoint projectors with per-partition coverage barriers, event/time flushing, retry, final flush, cancellation, and shutdown coordination
  - deterministic hooks for validating enqueue, persistence, retry, and flush ordering

- [x] update: [command/types.go](../../../pkg/processors/command/types.go)
  - extend: partition state with per-workspace absent, recovering, failed, and ready lifecycle state plus tail-recovered counter data
  - add: bounded per-partition workspace recovery scheduling and worker tracking without changing command serialization
  - use: the ID generator's last-record-ID accessor for in-memory workspace sequence state

- [x] update: [command/impl.go](../../../pkg/processors/command/impl.go)
  - update: partition recovery to read its checkpoint, retain the preceding event for reapplication, scan only the uncovered PLog tail, or perform a full bootstrap scan when the checkpoint is absent
  - update: merge recovered counters monotonically, reapply the last event, persist affected workspace snapshots first, and advance partition progress only afterward
  - add: lazy workspace recovery from shared snapshots with deduplicated attempts, bounded concurrency, retryable admission, and the existing retained-error retry pattern
  - preserve: authentication-before-recovery, service-context lifetime, stale-attempt protection, and reset-on-persistence-or-projector-failure behavior

- [x] update: [command/provide.go](../../../pkg/processors/command/provide.go)
  - inject: checkpoint storage and workspace-recovery concurrency into the command service
  - normalize: a zero workspace-recovery concurrency setting to the default of four for backward-compatible zero-valued configurations
  - update: the command pipeline to admit a recovered workspace immediately after partition admission and to enqueue its immutable checkpoint only after the complete store path succeeds
  - update: service shutdown to stop and join checkpoint and recovery workers before closing shared pipelines

- [x] update: [command/test_utils.go](../../../pkg/processors/command/test_utils.go)
  - extend: deterministic recovery controls to address partition and workspace attempts independently
  - add: checkpoint-worker gates, injected storage failures, flush observation, and wait helpers without timing sleeps

### VVM configuration and wiring

- [ ] update: [vvm/consts.go](../../../pkg/vvm/consts.go)
  - add: the default per-partition workspace recovery concurrency of four

- [ ] update: [vvm/types.go](../../../pkg/vvm/types.go)
  - add: the workspace-recovery concurrency field to VVM configuration

- [ ] update: [vvm/impl_cfg.go](../../../pkg/vvm/impl_cfg.go)
  - initialize: the new concurrency field from its default

- [ ] update: [vvm/provide.go](../../../pkg/vvm/provide.go)
  - wire: shared recovery-checkpoint storage and workspace-recovery concurrency into command-service construction

- [ ] regenerate: [vvm/wire_gen.go](../../../pkg/vvm/wire_gen.go)
  - run: `go generate ./pkg/vvm` after updating the Wire provider graph

## Quick start

The default configuration permits four concurrent workspace recoveries per partition; zero also selects this default. Override it before starting the VVM when a deployment needs a different positive bound:

```go
cfg := vvm.NewVVMDefaultConfig()
cfg.NumWSRecoverers = 8
```
