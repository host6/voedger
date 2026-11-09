# Context subsystem architecture: prod/apps/processing

Apps processing subsystem architecture for the command, query (v1 and v2), sync and async actualizer, scheduler, BLOB, and N10n pipelines that authenticate the caller, authorize against the workspace ACL, invoke extensions on application partitions, and return a response. Context-level overview: [arch.md](./arch.md).

## External actors

Systems:

- `*Client`
  - External caller invoking commands, queries, BLOB transfers, and N10n subscriptions over Voedger HTTP APIs.

## Scenarios overview

- **`Handle command`**
  - Command pipeline authenticates the caller, authorizes the command and CUDs, executes the extension, writes the PLog event and CUDs, runs sync actualizers, sends the response, and notifies async actualizers.

- **`Handle query`**
  - Query pipeline (v1 or v2) authenticates the caller, authorizes the query, invokes the query extension, and streams result rows to the responder.

- **`Run async actualizer`**
  - Async actualizer watches the PLog notification channel, reads new events, and invokes each triggered projector extension on its partition.

- **`Run scheduler job`**
  - Scheduler waits for the next cron tick, borrows the partition, and invokes the job extension.

- **`Handle BLOB transfer`**
  - BLOB processor authenticates the caller, authorizes the operation, registers or looks up BLOB metadata via a paired command on the cluster app, streams BLOB bytes to or from storage, and returns the result to the responder.

- **`Deliver N10n notification`**
  - The command pipeline updates the N10n broker with the new PLog offset; subscribers (async actualizers and SSE clients) receive the update on their channels.

## Components

### Layers

```text
External actors
    |
    +-- *Client
    |
    v
Entry points
    |
    +-- [Command processor]
    +-- [Query v1 processor]
    +-- [Query v2 processor]
    +-- [BLOB processor]
    +-- [Async actualizer]
    +-- [Scheduler]
    |
    v
In-pipeline operators
    |
    +-- [Sync actualizer]
    +-- [Authenticator call]
    +-- [ACL check]
    +-- [Legacy ACL fallback]
    |
    v
Shared infrastructure
    |
    +-- [App partitions engine]
    +-- [Bus responder]
    +-- [N10n broker]
    |
    v
State
    |
    +-- [(PLog)]
    +-- [(WLog)]
    +-- [(Records)]
    +-- [(Views)]
    +-- [(BLOB storage)]
```

### Entry points

- `[Command processor]`
  - Sync pipeline that borrows the partition, applies rate limits, authenticates, authorizes the command and parsed CUDs, executes the extension, runs the sync actualizer branch, validates and writes the PLog event and CUDs, and emits an N10n update on success.
  - Parses and processes every CUD present in a command request without imposing a fixed CUD-count limit. The [routing ingress request validator](../routing/arch-ingress.md#request-validator) bounds external request bodies before dispatch; apps retains per-CUD authorization and validation and commits the resulting event transactionally.
  - Path to file: [pkg/processors/command/provide.go](../../../../pkg/processors/command/provide.go)

- `[Query v1 processor]`
  - Sync pipeline behind the API v1 query route that borrows the partition, authorizes the query, invokes the query extension, and streams rows to the responder via a rows processor.
  - Path to file: [pkg/processors/query/impl.go](../../../../pkg/processors/query/impl.go)

- `[Query v2 processor]`
  - API v2 sync pipeline that dispatches by API path (`Queries`, `Views`, `Docs`, …) to the matching handler before invoking the extension and streaming rows.
  - Path to file: [pkg/processors/query2/impl.go](../../../../pkg/processors/query2/impl.go)

- `[BLOB processor]`
  - Pipeline that registers or reads BLOB metadata through a paired command on the cluster app and streams BLOB bytes between client and BLOB storage via a `bus.IRequestSender`.
  - Path to file: [pkg/processors/blobber/impl.go](../../../../pkg/processors/blobber/impl.go)

- `[Async actualizer]`
  - Per-projector goroutine triggered by `[N10n broker]` notifications: reads new `[(PLog)]` events and invokes the projector extension via `IAppPartition.Invoke`.
  - Path to file: [pkg/processors/actualizers/async.go](../../../../pkg/processors/actualizers/async.go)

- `[Scheduler]`
  - Per-job goroutine triggered by a cron tick: waits on the schedule, then invokes the job extension after `WaitForBorrow` on the partition.
  - Path to file: [pkg/processors/schedulers/impl_scheduler.go](../../../../pkg/processors/schedulers/impl_scheduler.go)

### In-pipeline operators

- `[Sync actualizer]`
  - Per-partition operator invoked from `[Command processor]` via `IAppPartition.DoSyncActualizer`; forks across registered sync projectors and applies their intents inside the command transaction.
  - Path to file: [pkg/processors/actualizers/impl.go](../../../../pkg/processors/actualizers/impl.go)

- `[Authenticator call]`
  - Calls `iauthnz.IAuthenticator.Authenticate` in the target Application and Workspace, retaining one authentication result on the command or query workpiece; component responsibilities are defined under [Request authentication result](#request-authentication-result)
  - Path to file: [pkg/iauthnz/authn-interface.go](../../../../pkg/iauthnz/authn-interface.go)

- `[ACL check]`
  - Per-pipeline operator that calls `IAppPartition.IsOperationAllowed` for the request operation kind (`Execute` for commands and queries, per-CUD kind for command CUDs); maps a deny to `403 Forbidden`.
  - Uses effective Principals and derived role names within the current request context; System checks match role kind and System QName without a Principal.WSID comparison
  - Path to file: [pkg/processors/command/impl.go](../../../../pkg/processors/command/impl.go)

- `[Legacy ACL fallback]`
  - Falls back to `oldacl.IsOperationAllowed` when the new ACL denies, kept as a temporary bridge for Air; logged as `newACL not ok, but oldACL ok` when only the legacy path allows.
  - Matches the reserved Guest using Payload's SubjectKind, canonical `sys.Guest` Login, and GuestWSID profile reference; removing Principal.WSID does not turn the Guest pattern into a match for every User Principal
  - Path to package: [pkg/processors/oldacl](../../../../pkg/processors/oldacl)

### Shared infrastructure

- `[App partitions engine]`
  - Borrow/invoke surface used by every entry point: `Borrow` / `WaitForBorrow` (`IAppPartitions.Borrow` / `WaitForBorrow`, pool sized to `EnginePoolSize[ProcessorKind]`), `Invoke` (`IAppPartition.Invoke`, resolves the extension and checks `ProcessorKind` compatibility), and `DoSyncActualizer` (`IAppPartition.DoSyncActualizer`, called by `[Command processor]` after writing the PLog event). Cross-subsystem definition: see [arch.md](./arch.md).
  - Path to file: [pkg/appparts/interface.go](../../../../pkg/appparts/interface.go)

- `[Bus responder]`
  - `bus.IRequestSender` / `IResponder` carry the response (single payload for commands, streamed rows for queries) back to the caller through a per-request channel.
  - Path to file: [pkg/bus/interface.go](../../../../pkg/bus/interface.go)

- `[N10n broker]`
  - In-memory broker that fans out projection-key updates to subscriber channels: `[Command processor]` publishes the new PLog offset, `[Async actualizer]` and SSE subscribers consume it.
  - Path to file: [pkg/in10nmem/impl.go](../../../../pkg/in10nmem/impl.go)

### State

- `[(PLog)]`
  - Partition-scoped event log appended by `[Command processor]` and read by `[Async actualizer]`.
  - Path to file: [pkg/istructs/interface.go](../../../../pkg/istructs/interface.go)

- `[(WLog)]`
  - Workspace-scoped event log appended by `[Command processor]`.
  - Path to file: [pkg/istructs/interface.go](../../../../pkg/istructs/interface.go)

- `[(Records)]`
  - Record storage updated by `[Command processor]` CUDs and read by `[Query v1 processor]` / `[Query v2 processor]`.
  - Path to file: [pkg/istructs/interface.go](../../../../pkg/istructs/interface.go)

- `[(Views)]`
  - Projection view storage updated by `[Sync actualizer]` and `[Async actualizer]` intents.
  - Path to file: [pkg/istructs/interface.go](../../../../pkg/istructs/interface.go)

- `[(BLOB storage)]`
  - BLOB byte storage targeted by `[BLOB processor]`.
  - Path to package: [pkg/iblobstorage](../../../../pkg/iblobstorage)

### Request authentication result

The authentication result is scoped to one Application and request Workspace. Command and query v1/v2 workpieces retain it for that request and expose exactly Payload and Principals to the consumers below. A reused workpiece clears the whole result, and an authentication error prevents consumption of a partially populated result.

| Consumer                                      | Result used          | Responsibility                                                                            |
|-----------------------------------------------|----------------------|-------------------------------------------------------------------------------------------|
| Command and query v1/v2 ACL and System checks | Effective Principals | Preserve decisions in the request Application and Workspace                               |
| N10n subscription enforcement                 | Effective Principals | Authenticate separately for each subscription Workspace and select roles from that result |
| RequestSubject storage and identity logging | Payload | Project original Subject identity independently of effective Principal composition |
| Principal-token enrichment | Payload and Principals | Preserve original grants and add effective roles using the request Workspace |

N10n does not reuse an effective Principal set across subscription Workspaces. Other callers of the authentication boundary migrate to the result contract together and consume only the fields they need. Anonymous-only function shortcuts retain their existing token bypass and Anonymous-only authorization policy; they initialize reserved Guest identity for request state without manufacturing token-carried grants or additional effective roles.

Identity and enrichment semantics are owned by [AuthnResult](../auth/context.md#authnresult) and [PrincipalToken](../auth/context.md#principaltoken).

RequestSubject keeps its existing storage name, key, field names, and field types. `Kind`, `Name`, and `ProfileWSID` project Payload's SubjectKind, canonical Login, and profile reference; `Token` continues to come from the request token provider. User and Device identity comes from validated claims, Guest uses the reserved identity, and API-token and System identity remains available even when composition omits User/Device Principals. A null profile reference remains null and is never replaced by the request Workspace. The storage does not decode the token or scan Principals to recover identity.


## Scenarios

### Handle command

```text
*Client
  -> [Command processor]
  -> [App partitions engine].Borrow(Command)
  -> [Authenticator call]
  -> [ACL check] (+ [Legacy ACL fallback])
  -> [App partitions engine].Invoke(command)
  -> append [(PLog)], store [(Records)] CUDs, append [(WLog)]
  -> [App partitions engine].DoSyncActualizer -> [Sync actualizer] -> apply [(Views)] intents
  -> response via [Bus responder]
  -> [N10n broker].Update(PLog offset)
```

### Handle query

```text
*Client
  -> [Query v1 processor] | [Query v2 processor]
  -> [App partitions engine].Borrow(Query)
  -> [Authenticator call]
  -> [ACL check] (+ [Legacy ACL fallback])
  -> [App partitions engine].Invoke(query/view/doc handler)
  -> stream rows via [Bus responder]
```

### Run async actualizer

```text
[N10n broker]: Update(PLog offset)
  -> [Async actualizer] wakes on its channel
  -> reads new [(PLog)] events
  -> [App partitions engine].Borrow(Actualizer)
  -> [App partitions engine].Invoke(projector) per triggered event
  -> apply intents to [(Views)] (and emit further N10n updates)
```

### Run scheduler job

```text
[Scheduler] cron tick
  -> [App partitions engine].WaitForBorrow(Scheduler)
  -> [App partitions engine].Invoke(job)
  -> apply intents to [(Views)] / [(Records)]
```

### Handle BLOB transfer

```text
*Client
  -> [BLOB processor]
  -> [Authenticator call]
  -> [ACL check] (+ [Legacy ACL fallback])
  -> paired command on sys.cluster (register/lookup BLOB metadata)
       -> [Bus responder] dispatches internal request
       -> [Command processor] handles the paired command
  -> stream bytes to/from [(BLOB storage)]
  -> response via [Bus responder]
```

### Deliver N10n notification

```text
[Command processor] commits a command successfully
  -> [N10n broker].Update(ProjectionKey{App, PLogUpdates, WS=partitionID}, offset)
  -> [Async actualizer] channel wakes (see Run async actualizer)
  -> SSE subscribers receive the update through [N10n broker].WatchChannel
```

## Cross-cutting concerns

### Observability

- Every authorization log produced by Command processor, Query v1 processor, Query v2 processor, or Legacy ACL fallback retains its existing severity and decision details, including operation kind, target qualified name, and relevant allow/deny policy information
- Every such log that reports Workspace identity distinguishes the request Workspace from Subject ProfileWorkspace, using request context and Payload respectively; Principal formatting no longer labels role or Subject applicability with Principal.WSID
- Every authentication-result consumer excludes bearer-token contents from logs

Legacy deny diagnostics retain Verbose severity and the existing `old-style ... -> deny` message structure. The fallback success diagnostic retains Verbose severity and `newACL not ok, but oldACL ok` wording. Both obtain Workspace and profile context explicitly rather than from Principal formatting.

### Testing

- Every authentication-result consumer has focused coverage for the fields it uses and for request isolation; verification stays in the owning package rather than duplicating auth policy in processor tests
- Every migrated ACL enforcement point preserves existing allow/deny behavior in its relevant regression tests
