# Feature technical design: Partition and workspace recovery

Technical design for [Partition and workspace recovery](./recovery.feature).
Normal offset and identifier issuance is specified separately in
[Sequence allocation](./sequences--td.md).

## Key concepts

- **Recovery checkpoint**: a persisted log offset used as an inclusive recovery
  scan start. It accelerates recovery but is not authoritative sequence state.
- **Partition checkpoint**: `lastPLogOffset`, the latest handled PLog offset
  selected by initial recovery or the asynchronous checkpoint projector.
- **Workspace checkpoint**: `wlogIDBearingOffset`, the latest ID-bearing WLog
  offset found by its producer. It need not be the latest ID-bearing event in
  the WLog and is never treated as the WLog tail.
- **Zero-value initialization sentinel**: a present zero partition checkpoint
  marks an initialized empty PLog; a present zero workspace checkpoint marks an
  encountered workspace with no ID-bearing event. Either zero selects one scan
  from `FirstOffset` without validation or invalid-checkpoint logging.
- **ID-bearing event**: an event that allocates at least one new non-singleton
  record ID at or above `FirstUserRecordID`.
- **Initial recovery**: bootstrap-time computation and persistence of missing
  checkpoints for every declared application partition. It does not publish
  runtime partition or workspace state.
- **Initial-recovery completion marker**: presence of a partition checkpoint,
  including a zero-valued one. Workspace checkpoints derived from a partition
  are stored before this marker.
- **Inclusive suffix scan**: `ReadPLog` or `ReadWLog` begins at the checkpointed
  event and continues through the actual log tail.
- **Checkpoint fallback**: one scan from `FirstOffset` after a nonzero checkpoint
  fails validation. Recovery does not repair the stored checkpoint.
- **Partition recovery**: service-scoped, request-triggered restoration of the
  next PLog offset followed by reapplication of the final event.
- **Workspace recovery**: lazy, request-triggered restoration of one workspace's
  next WLog offset and ordinary record-ID generator.

## External actors

Systems:

- `*Client`
  - Submits commands whose partition and workspace state may require recovery.

## Components

### Layers

```text
Bootstrap
    |
    +-- [Bootstrap operator]
    +-- [Checkpoint initial recovery]
    |
    v
Request recovery
    |
    +-- [Command processor]
    +-- [Partition recovery manager]
    +-- [Workspace recovery manager]
    +-- [Offsets bookkeeping]
    +-- [Record ID generator]
    |
    v
Checkpointing
    |
    +-- [Recovery checkpoint projector]
    +-- [(Recovery checkpoint storage)]
    |
    v
Application operations
    |
    +-- [Application events]
    +-- [Application records]
    +-- [App partitions engine]
```

### Bootstrap

- `[Bootstrap operator]`
  - Registers each application with `[App partitions engine]`, invokes
    `[Checkpoint initial recovery]`, and only then deploys the application's
    partition workers. A returned setup or cancellation error stops bootstrap;
    partition-scoped errors have already been logged and suppressed by the
    initializer, so deployment and readiness continue.
  - impl: [pkg/btstrp/impl.go](../../../../pkg/btstrp/impl.go)

- `[Checkpoint initial recovery]`
  - `checkpoints.IInitialRecovery.Ensure(ctx, appQName, numPartitions)` obtains
    the application's event storage, samples `runtime.GOMAXPROCS(0)`, and checks
    every declared partition in `0..numPartitions-1`.
  - A present partition checkpoint skips that partition. Each missing partition
    is scheduled under the sampled bound and scanned from `FirstOffset` through
    the complete PLog. The attempt retains the actual final PLog offset and the
    latest ID-bearing WLog offset for every WSID encountered.
  - It stores a workspace checkpoint for every encountered WSID, using zero when
    no ID-bearing event was found, and stores the partition checkpoint last,
    using zero for an empty PLog. A failed attempt therefore leaves no completion
    marker and is repeated on a later bootstrap.
  - It logs and suppresses every read or checkpoint-storage error inside a
    scheduled partition attempt, waits for all scheduled attempts, and does not
    cancel siblings. Only app-structures setup failure before scheduling or
    bootstrap-context cancellation is returned.
  - decl: [pkg/checkpoints/types.go#IInitialRecovery](../../../../pkg/checkpoints/types.go)

### Request recovery

- `[Command processor]`
  - Runs partition recovery admission after authentication/workspace checks and
    workspace recovery admission after a recovered partition is available. A
    command does not execute on the request that starts either recovery.
  - impl: [pkg/processors/command/provide.go](../../../../pkg/processors/command/provide.go)
  - impl: [pkg/processors/command/impl.go](../../../../pkg/processors/command/impl.go)

- `[Partition recovery manager]`
  - Owns states keyed by application and partition. Concurrent requests share
    one attempt; a failure is retained for the next request, which receives the
    failure while starting exactly one retry. Partition recovery has no
    configured concurrency bound.
  - Uses the service context. Reset detaches old state so stale completion cannot
    publish into a replacement. Shutdown cancels and joins all workers.
  - impl: [pkg/processors/command/recovery_manager.go](../../../../pkg/processors/command/recovery_manager.go)
  - impl: [pkg/processors/command/impl.go](../../../../pkg/processors/command/impl.go)

- `[Workspace recovery manager]`
  - Lives within a recovered partition, owns states keyed by application,
    partition, and WSID, and shares exactly `NumWSRecoverers` slots. Saturation
    returns `503` without creating state or queueing work; zero admits none.
  - Reuses the retained-failure, retry, stale-completion, service-context, and
    shutdown lifecycle of `[Partition recovery manager]`.
  - impl: [pkg/processors/command/recovery_manager.go](../../../../pkg/processors/command/recovery_manager.go)
  - impl: [pkg/processors/command/impl.go](../../../../pkg/processors/command/impl.go)

- `[Offsets bookkeeping]`
  - Holds `appPartition.nextPLogOffset` and `workspace.NextWLogOffset`. Recovery
    sets each value from the actual last callback plus one, or `FirstOffset` for
    an empty complete scan.
  - impl: [pkg/processors/command/types.go](../../../../pkg/processors/command/types.go)

- `[Record ID generator]`
  - Starts at `FirstUserRecordID`; workspace recovery advances it from the latest
    newly allocated non-singleton ID encountered in the complete WLog suffix.
  - impl: [pkg/istructsmem/idgenerator.go](../../../../pkg/istructsmem/idgenerator.go)

### Checkpointing

- `[Recovery checkpoint projector]`
  - The single built-in asynchronous `sys.ProjectorRecoveryCheckpoint` stores a
    partition checkpoint every 100 PLog events and replaces the workspace
    checkpoint whenever it sees an ID-bearing event. Both initial recovery and
    this projector use the same ID-bearing-event predicate.
  - impl: [pkg/checkpoints/impl.go](../../../../pkg/checkpoints/impl.go)
  - impl: [pkg/checkpoints/provide.go](../../../../pkg/checkpoints/provide.go)

- `[(Recovery checkpoint storage)]`
  - Last-write-wins storage for independent partition and workspace JSON values.
    Partition keys include application and partition; workspace keys include
    application and WSID. Recovery reads but never rewrites checkpoints.
  - decl: [pkg/checkpoints/types.go](../../../../pkg/checkpoints/types.go)
  - impl: [pkg/vvm/storage/impl_recoverycheckpoint.go](../../../../pkg/vvm/storage/impl_recoverycheckpoint.go)

### Application operations

- `[Application events]`
  - Supplies complete or inclusive-suffix PLog/WLog reads. Initial recovery reads
    PLogs only; partition recovery reads a PLog; workspace recovery reads a WLog.
  - decl: [pkg/istructs/interface.go](../../../../pkg/istructs/interface.go)
  - impl: [pkg/istructsmem/impl.go](../../../../pkg/istructsmem/impl.go)

- `[Application records]`
  - Reapplies the final recovered partition event through the event-reapply path;
    record and WLog writes are unconditional and consume no new PLog offset.
  - impl: [pkg/istructsmem/impl.go](../../../../pkg/istructsmem/impl.go)

- `[App partitions engine]`
  - Registers application structures before initial recovery, starts partition
    actualizers and schedulers afterward, and invokes synchronous projectors when
    partition recovery reapplies the final event.
  - decl: [pkg/appparts/interface.go](../../../../pkg/appparts/interface.go)
  - impl: [pkg/appparts/impl.go](../../../../pkg/appparts/impl.go)

## Scenarios

### Initial recovery

#### Missing checkpoints are computed for every declared partition

```text
[Bootstrap operator]
  -> [App partitions engine]: register application with partitions 0..2
  -> [Checkpoint initial recovery]: Ensure(application, 3)
      -> [(Recovery checkpoint storage)]: partition 0 present -> skip
      -> [Application events]: scan complete PLogs for missing partitions 1 and 2
      -> [(Recovery checkpoint storage)]: partition 1 workspace checkpoints 10=6, 11=0
      -> [(Recovery checkpoint storage)]: partition 1 checkpoint=50 (completion marker)
      -> [(Recovery checkpoint storage)]: partition 2 checkpoint=0 (empty completion marker)
  -> [App partitions engine]: deploy partitions 0..2
```

Workspace writes for one partition complete before its partition marker. The
relative ordering of different partition attempts is unconstrained.

#### Initial recovery initializes checkpoints but not runtime sequence state

```text
[Bootstrap operator]
  -> [Checkpoint initial recovery]: persist missing checkpoints only
  -> [App partitions engine]: deploy partition workers; server ready
*Client
  -> [Command processor]: first command
      -> [Partition recovery manager]: start recovery -> 503
  -> [Command processor]: retry after partition recovery
      -> [Workspace recovery manager]: start lazy workspace recovery -> 503
  -> [Command processor]: retry after workspace recovery -> command succeeds
```

#### Initial recovery failures do not prevent server readiness

```text
[Checkpoint initial recovery]
  -> scheduled missing partitions, bounded by runtime.GOMAXPROCS(0)
      -> [Application events]: one or every PLog scan fails
      - log each partition-scoped failure; do not write its partition marker
      - let every sibling attempt finish; wait for all scheduled attempts
  -> [Bootstrap operator]: Ensure returns no partition-scoped error
      -> [App partitions engine]: deploy all declared partitions; server ready
*Client
  -> [Command processor]: command for failed partition
      -> [Partition recovery manager]: start request recovery -> 503
  -> [Command processor]: request after persistent failure -> 500 and one retry
  -> [Command processor]: request while retry runs -> 503
```

If setup fails before partition scheduling or the bootstrap context is canceled,
`Ensure` returns the error and `[Bootstrap operator]` does not deploy that
application's partitions. Readiness is otherwise independent of how many
scheduled partition attempts failed, including all of them.

### Partition checkpoint

#### Partition recovery starts from a checkpoint

```text
*Client
  -> [Command processor]: command
      -> [Partition recovery manager]: start -> 503
          -> [(Recovery checkpoint storage)]: absent, zero, or valid lastPLogOffset
          -> [Application events]: ReadPLog(checkpoint or FirstOffset, through tail)
          -> [Offsets bookkeeping]: nextPLogOffset = actual tail + 1
          -> [Application records]: reapply final event when present
  -> [Command processor]: later request enters workspace recovery
```

Absent and zero checkpoints select `FirstOffset` directly. A nonzero checkpoint
must equal the first returned event offset.

#### Invalid partition checkpoint falls back to the PLog beginning

```text
[Partition recovery manager]
  -> [(Recovery checkpoint storage)]: nonzero lastPLogOffset
  -> [Application events]: inclusive suffix returns no callback or a different first offset
  - log cp.partition_recovery.checkpoint.invalid
  -> [Application events]: one fallback ReadPLog(FirstOffset, through tail)
  -> [Offsets bookkeeping]: restore nextPLogOffset from fallback's actual tail
```

#### Empty PLog recovery starts at FirstOffset

```text
[Partition recovery manager]
  -> [(Recovery checkpoint storage)]: absent or zero checkpoint
  -> [Application events]: ReadPLog(FirstOffset, through tail) -> empty
  -> [Offsets bookkeeping]: nextPLogOffset = FirstOffset
  - publish partition without reapplication or invalid-checkpoint log
```

#### Invalid partition checkpoint over an empty PLog

```text
[Partition recovery manager]
  -> [Application events]: read nonzero checkpoint -> no callback
  - log invalid checkpoint
  -> [Application events]: fallback from FirstOffset -> empty
  -> [Offsets bookkeeping]: nextPLogOffset = FirstOffset
```

### Workspace checkpoint

#### Workspace recovery starts from a checkpoint

```text
*Client
  -> [Command processor]: command after partition recovery
      -> [Workspace recovery manager]: reserve slot; start -> 503
          -> [(Recovery checkpoint storage)]: absent, zero, or valid wlogIDBearingOffset
          -> [Application events]: ReadWLog(checkpoint or FirstOffset, through tail)
          -> [Offsets bookkeeping]: NextWLogOffset = actual tail + 1
          -> [Record ID generator]: advance from latest ID-bearing event in suffix
  -> [Command processor]: later retry succeeds with following offset and record ID
```

#### Workspace recovery starts from an ID-bearing checkpoint at the WLog tail

```text
[Workspace recovery manager]
  -> [Application events]: ReadWLog(ID-bearing tail checkpoint, through tail)
      - first and final callback are the checkpoint event
  -> [Offsets bookkeeping]: NextWLogOffset = checkpoint offset + 1
  -> [Record ID generator]: next ID = greatest new ordinary ID + 1
```

#### Non-ID-bearing suffix events advance only the WLog offset

```text
[Workspace recovery manager]
  -> [Application events]: scan from ID-bearing checkpoint through CUD-free,
                           singleton-only, or existing-record-update suffix
  -> [Offsets bookkeeping]: advance from the actual final suffix event
  -> [Record ID generator]: advance only from the earlier ID-bearing event
```

#### Workspace without user record IDs starts allocation at FirstUserRecordID

```text
[Workspace recovery manager]
  -> [Application events]: complete WLog scan finds no ID-bearing event
  -> [Offsets bookkeeping]: NextWLogOffset = actual tail + 1
  -> [Record ID generator]: retain FirstUserRecordID
```

#### Invalid workspace checkpoint falls back to the WLog beginning

```text
[Workspace recovery manager]
  -> [Application events]: inclusive suffix from nonzero checkpoint
      - no callback, different first offset, or first event is not ID-bearing
  - log cp.workspace_recovery.checkpoint.invalid
  -> [Application events]: one fallback ReadWLog(FirstOffset, through tail)
  -> [Offsets bookkeeping] + [Record ID generator]: restore from complete fallback
```

### Empty WLog

#### Empty WLog recovery starts at FirstOffset and FirstUserRecordID

```text
[Workspace recovery manager]
  -> [(Recovery checkpoint storage)]: absent or zero checkpoint
  -> [Application events]: ReadWLog(FirstOffset, through tail) -> empty
  -> [Offsets bookkeeping]: NextWLogOffset = FirstOffset
  -> [Record ID generator]: next ID = FirstUserRecordID
```

#### Invalid workspace checkpoint over an empty WLog

```text
[Workspace recovery manager]
  -> [Application events]: read nonzero checkpoint -> no callback
  - log invalid checkpoint
  -> [Application events]: fallback from FirstOffset -> empty
  -> [Offsets bookkeeping]: NextWLogOffset = FirstOffset
  -> [Record ID generator]: next ID = FirstUserRecordID
```

### Workspace recovery admission

#### Duplicate requests share one workspace recovery

```text
*Client
  -> [Workspace recovery manager]: first request; reserve slot and start -> 503
  -> [Workspace recovery manager]: duplicate request; share attempt -> 503
  - exactly one workspace-recovery start is logged
  -> [Workspace recovery manager]: later request after completion -> recovered value
```

#### Recovery limit rejects without queueing and permits a later retry

```text
*Client
  -> [Workspace recovery manager]: workspace 42; reserve only slot -> 503
  -> [Workspace recovery manager]: workspace 43; no slot -> 503
      - create no state and queue no worker for workspace 43
  -> [Workspace recovery manager]: after workspace 42 releases slot,
                                   a new request starts workspace 43 -> 503
```

#### Zero recovery limit rejects workspace recovery

```text
*Client
  -> [Workspace recovery manager]: unrecovered workspace with zero slots
  <- [Command processor]: 503; log concurrency limit; start no recovery
```

#### Failed workspace recovery is reported and retried once

```text
*Client
  -> [Workspace recovery manager]: start attempt -> 503
      -> [Application events]: WLog read fails; retain failure
  -> [Workspace recovery manager]: next request gets retained failure;
                                   reserve slot and start one retry -> 500
  -> [Workspace recovery manager]: request while retry runs -> 503
  -> [Workspace recovery manager]: request after completion -> recovered value
```

## Sequence diagrams

### Request-driven partition then workspace recovery

```mermaid
sequenceDiagram
    participant Client as *Client
    participant CP as [Command processor]
    participant Part as [Partition recovery manager]
    participant WS as [Workspace recovery manager]

    Client->>CP: Command for unrecovered partition/workspace
    CP->>Part: getOrStart(partition)
    Part-->>CP: 503; recover partition asynchronously
    CP-->>Client: 503
    Client->>CP: Retry after partition recovery
    CP->>Part: recovered partition
    CP->>WS: getOrStart(workspace)
    WS-->>CP: 503; recover workspace asynchronously
    CP-->>Client: 503
    Client->>CP: Retry after workspace recovery
    WS-->>CP: recovered workspace
    CP-->>Client: Command result
```

## Cross-cutting concerns

### Failure isolation and shutdown

- Every scheduled `[Checkpoint initial recovery]` partition attempt owns its
  read and writes; its failure leaves the partition marker absent, is logged,
  and does not cancel sibling attempts or prevent readiness.
- Every bootstrap-context cancellation stops scheduling, cancels in-flight
  initialization, waits for started work, and is returned to `[Bootstrap operator]`.
- Every request-recovery attempt uses the VVM service context rather than request
  cancellation. Command-service shutdown cancels and joins partition and
  workspace workers before clearing their state.
- Every recovery-manager key has at most one current attempt; reset may detach
  an attempt, whose later stale completion cannot overwrite replacement state.

### Observability

- Every request-driven partition log uses `vapp=sys/voedger`,
  `extension=sys._Recovery`, and `partid`; workspace logs add `wsid`.
- Every request admission or retained failure is emitted through `cp.error` with
  the affected partition or workspace in the message.
- Every malformed checkpoint is logged before its single `FirstOffset` fallback;
  read, reapply, start, and completion stages follow
  [Logging](./logging--td.md#command-processor).
- Every initial-recovery attempt log identifies the application and partition,
  distinguishes PLog-read, workspace-checkpoint-write, and
  partition-checkpoint-write failures, and states the partition-scoped outcome.
  The application completion log is emitted only after every scheduled attempt
  has finished.

### Performance and persistence

- Every application's initial recovery has at most the `runtime.GOMAXPROCS(0)`
  value sampled for that `Ensure` call in flight; applications are initialized
  in bootstrap order.
- Every workspace recovery manager admits at most `NumWSRecoverers` concurrent
  attempts for its partition and never queues rejected work.
- Every nonzero recovery checkpoint is an inclusive scan start; correctness
  requires reading through the actual tail. A long non-ID-bearing WLog suffix
  therefore increases workspace recovery time without changing record-ID state.
- Every checkpoint-storage write is last-write-wins. Initial recovery establishes
  missing completion markers; the asynchronous projector advances checkpoints;
  request-driven recovery never writes them.

### Testing

- Feature scenarios are traced to `recovery: scn:` tests in
  [VIT recovery tests](../../../../pkg/sys/it/impl_recovery_test.go), with mocked
  command-processor coverage only for empty logs and lifecycle control that VIT
  cannot construct deterministically.
- Initial-recovery scheduling, ordering, failure isolation, and cancellation are
  covered by the planned `pkg/checkpoints/initial_recovery_test.go` tests and
  [bootstrap tests](../../../../pkg/sys/it/impl_bootstrap_test.go).
- Generic deduplication, limits, retry, reset, stale completion, context, and
  shutdown behavior is covered by
  [recovery-manager tests](../../../../pkg/processors/command/recovery_manager_test.go).

