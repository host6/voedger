# Context subsystem architecture: prod/apps/deployment

Apps deployment subsystem architecture for registering built-in and sidecar applications, initializing missing recovery checkpoints, deploying partitions, and rejecting incompatible redeployments. Context-level overview: [arch.md](./arch.md).

## External actors

Roles:

- `@VADeveloper`
  - Supplies built-in app packages and sidecar app modules consumed at deployment time.

- `@Admin`
  - Operates the VVM process and triggers redeployment by restarting the VVM with updated app definitions.

## Scenarios overview

- **`Bootstrap VVM`**
  - On VVM startup initialize the cluster app workspace, register each application's structures, initialize its missing recovery checkpoints, and deploy only the returned eligible partition IDs.

- **`Bootstrap with an unavailable cluster partition`**
  - Validate stored deployment descriptors read-only, deploy eligible partitions of existing compatible apps, and defer command-dependent provisioning without blocking readiness.

- **`Register app`**
  - The cluster app validates compatibility against the previously stored `cluster.App` descriptor and writes the descriptor on first deployment.

- **`Initialize recovery checkpoints`**
  - Before partition workers start, process every declared partition under a per-application CPU bound and wait for all scheduled attempts. Return deployable IDs and partition errors for bootstrap to log; partition failures do not block readiness.

- **`Deploy app partitions`**
  - App registration builds extension engine pools without partition runtimes. Partition deployment creates runtimes, asynchronous projector actualizers, and schedulers only for the supplied eligible IDs.

- **`Reject incompatible redeployment`**
  - Schema declarations and the deployment descriptor are cross-checked on every redeployment so a partially upgraded process cannot start with mismatched extensions or partition layout.

Undeploy is not implemented: applications and partitions live for the lifetime of the VVM process; redeployment is performed by restarting the VVM with updated app definitions.

## Components

### Layers

```text
External actors
    |
    +-- @VADeveloper
    +-- @Admin
    |
    v
Bootstrap
    |
    +-- [Bootstrap operator]
    +-- [Checkpoint initial recovery]
    |
    v
Cluster app
    |
    +-- [c.cluster.DeployApp]
    +-- [Deployment descriptor validator]
    +-- [cluster.App uniqueness check]
    |
    v
App partitions engine
    |
    +-- [DeployApp]
    +-- [DeployAppPartitions]
    +-- [Extension presence validator]
    |
    v
Schema build
    |
    +-- [VSQL parser]
    +-- [AppDef builder]
    |
    v
State
    |
    +-- [(cluster.App)]
    +-- [(AppStructs)]
```

### Bootstrap

- `[Bootstrap operator]`
  - Runs `btstrp.Bootstrap` with the VVM service context. App registration precedes checkpoint initialization; only returned deployable IDs are passed directly to `[DeployAppPartitions]`, after all scheduled attempts finish. An empty slice skips deployment. The gate applies to cluster, built-in, and sidecar apps, initialized sequentially in bootstrap order.
  - Logs each returned partition-error map entry once through `bootstrap.initial_recovery.error`, with app QName, partition ID, failed operation, and underlying cause. The map is used only for logging, not retained in app partitions or propagated to requests. A fatal setup/context error prevents deployment from that invocation, even if some returned IDs are ready; collected partition errors can still be logged.
  - Uses `[Deployment descriptor validator]` directly when the cluster partition is excluded. It sends no deployment commands to that partition and defers work requiring them instead of retrying until startup hangs.
  - impl: [pkg/btstrp/impl.go](../../../../pkg/btstrp/impl.go)

- `[Checkpoint initial recovery]`
  - `checkpoints.IInitialRecovery.InitializeCheckpoints(ctx, appQName, numPartitions)` returns `([]istructs.PartitionID, map[istructs.PartitionID]error, error)`: deployable IDs, operation-specific errors keyed by failed partition ID, and an optional fatal app-structures setup or context error. With a nil fatal error, the slice and map are disjoint and cover `0..numPartitions-1` exactly once. An empty slice and complete failure map with a nil fatal error are valid.
  - Initializes persistent checkpoints only, not runtime sequence state. Checkpoint calculation, completion-marker persistence, wrapped error details, and cancellation behavior are specified in [Initial recovery](./recovery--td.md#initial-recovery). Partition-failure logging belongs to `[Bootstrap operator]`.
  - decl: [pkg/checkpoints/types.go](../../../../pkg/checkpoints/types.go)

### Cluster app

- `[c.cluster.DeployApp]`
  - Command on the cluster app workspace that takes `AppDeploymentDescriptor` (`AppQName`, `NumPartitions`, `NumAppWorkspaces`), rejects redeployment of `sys.cluster`, and uses `[Deployment descriptor validator]` for compatibility. Only this healthy command path creates a missing `cluster.App` record and provisions app workspaces; an existing compatible descriptor does not repeat provisioning.
  - impl: [pkg/cluster/impl_deployapp.go](../../../../pkg/cluster/impl_deployapp.go)

- `[Deployment descriptor validator]`
  - Shared read-only lookup and compatibility check used by `[c.cluster.DeployApp]` and `[Bootstrap operator]`. Uses `[cluster.App uniqueness check]` and reads the stored descriptor; distinguishes absent, compatible, incompatible, and lookup-error outcomes without provisioning or application event writes.
  - Mismatched `NumPartitions` or `NumAppWorkspaces` preserves the existing `409 Conflict` error and message. The unavailable-cluster path does not bypass compatibility checks; incompatibility and lookup errors still stop bootstrap.
  - impl: [pkg/cluster/impl_deployapp.go](../../../../pkg/cluster/impl_deployapp.go)

- `[cluster.App uniqueness check]`
  - Looks up the `cluster.App` WDoc by unique `AppQName` for `[Deployment descriptor validator]`.
  - impl: [pkg/cluster/appws.vsql](../../../../pkg/cluster/appws.vsql)

### App partitions engine

- `[DeployApp]`
  - Registers an application with the engine, builds `IAppStructs` (built-in path via `IAppStructsProvider.BuiltIn`, sidecar path via `IAppStructsProvider.New` keyed by `extModuleURLs`), and constructs per-`ProcessorKind` extension engine pools sized by `EnginePoolSize`. Redeploying the same app panics.
  - Retains the full declared partition count and routing metadata even when no partitions deploy. Registration creates no `appPartitionRT`, asynchronous actualizer, or scheduler; building app structures and engine pools is not partition deployment.
  - impl: [pkg/appparts/impl.go](../../../../pkg/appparts/impl.go)

- `[DeployAppPartitions]`
  - For each supplied deployable ID creates an `appPartitionRT`, then concurrently deploys per-partition actualizers and schedulers using `IActualizerRunner` / `ISchedulerRunner`. Excluded IDs have no runtime partition, asynchronous projector (including the checkpoint projector), or scheduled job.
  - Uses the existing `IAppPartitions` contract and errors without a deployment-error registry. `Borrow` for an excluded ID returns the existing `appparts.ErrNotFound`; the command processor returns `500 Internal Server Error` with `partition not found`. A missing application instead returns `400 Bad Request` with `application not found`. Identity is carried by structured log attributes, not repeated in these request errors. The complete classification policy is specified in [recovery request handling](./recovery--td.md#recovery-on-command-request).
  - decl: [pkg/appparts/interface.go](../../../../pkg/appparts/interface.go)
  - impl: [pkg/appparts/impl.go](../../../../pkg/appparts/impl.go)

- `[Extension presence validator]`
  - Cross-checks the parsed `IAppDef` against the built-in extension registry so VSQL-declared extensions exist in code and stateless code-only extensions are tied to imported packages.
  - Path to file: [pkg/appparts/impl_app.go](../../../../pkg/appparts/impl_app.go)

### Schema build

- `[VSQL parser]`
  - Parses each `parser.PackageFS` into a `PackageSchemaAST` and merges them into an `AppSchemaAST` per app.
  - Path to file: [pkg/parser/provide.go](../../../../pkg/parser/provide.go)

- `[AppDef builder]`
  - Materializes the merged schema into `appdef.IAppDef` via `parser.BuildAppDefs` and `builder.New`; the result is passed to `[DeployApp]`.
  - Path to file: [pkg/vvm/impl_builder.go](../../../../pkg/vvm/impl_builder.go)

### State

- `[(cluster.App)]`
  - WDoc in the cluster app workspace holding `AppQName`, `NumPartitions`, `NumAppWorkspaces` with a unique constraint on `AppQName`; written on first deployment and read on every redeployment.
  - Path to file: [pkg/cluster/appws.vsql](../../../../pkg/cluster/appws.vsql)

- `[(AppStructs)]`
  - In-memory per-app structures (events, records, view records, resources, validators) used by the processing subsystem; created during `[DeployApp]`.
  - Path to file: [pkg/istructsmem/appstruct-types.go](../../../../pkg/istructsmem/appstruct-types.go)

## Scenarios

### Bootstrap VVM

```text
[Bootstrap operator]
  -> initClusterAppWS -> [(AppStructs)] for sys.cluster
  -> [DeployApp](sys.cluster)
  -> [Checkpoint initial recovery](sys.cluster, declared partition count)
       - log each returned partition error once
       - fatal error -> stop bootstrap; do not deploy returned IDs
       - eligible cluster ID -> [DeployAppPartitions](sys.cluster, deployable IDs)
       - no eligible cluster ID -> Bootstrap with an unavailable cluster partition
  -> if cluster partition deployed:
       for each built-in/sidecar app:
         [c.cluster.DeployApp]: validate descriptor; provision only if absent
       for each built-in/sidecar app, sequentially:
         [DeployApp](app, full declared partition count)
         -> [Checkpoint initial recovery](app, NumParts)
              - log each returned partition error once
              - fatal error -> stop bootstrap; do not deploy returned IDs
         -> [DeployAppPartitions](app, deployable IDs), only when nonempty
  - ready after bootstrap completes, even if any or all partition attempts failed
```

### Bootstrap with an unavailable cluster partition

```text
[Bootstrap operator]
  - sys.cluster registered; its partition excluded by initial recovery
  - do not call or retry c.cluster.DeployApp against the excluded partition
  -> for each configured built-in/sidecar app:
       [Deployment descriptor validator]
         -> [cluster.App uniqueness check] -> [(cluster.App)]: read only
         - lookup error or incompatible descriptor -> stop bootstrap
         - compatible descriptor:
             [DeployApp](app, full declared partition count)
             -> [Checkpoint initial recovery](app, NumParts)
                  - log each returned partition error once
                  - fatal error -> stop bootstrap; do not deploy returned IDs
             -> [DeployAppPartitions](app, deployable IDs), only when nonempty
         - descriptor absent:
             [DeployApp](app, full declared partition count): register for routing only
             - log deferred first-deployment/provisioning work
             - no descriptor writes, missing-workspace initialization, or partition deployment
  - ready after read-only validation and eligible partition initialization complete
```

Deferred provisioning is retried on a later bootstrap when the cluster partition
is available. It is not treated as successfully completed, and a newly configured
app without a persisted descriptor stays registered but undeployed for this run.

### Register app

```text
[c.cluster.DeployApp]
  -> [Deployment descriptor validator]
       -> [cluster.App uniqueness check] -> [(cluster.App)]
       - found, incompatible -> existing 409 Conflict
       - lookup error -> fail
       - found, compatible -> ok; do not provision again
       - absent -> return to command:
           insert [(cluster.App)] {AppQName, NumPartitions, NumAppWorkspaces}
           provision app workspaces
```

### Initialize recovery checkpoints

```text
[Bootstrap operator]
  -> [Checkpoint initial recovery]: InitializeCheckpoints(ctx, appQName, NumPartitions)
      - sample worker limit = runtime.GOMAXPROCS(0) once for this application's call
      - enumerate complete declared range 0..NumPartitions-1
      - present completion marker, including zero -> add ID to deployable IDs
      - process missing partitions with at most worker-limit attempts in flight:
          scan each PLog sequentially from FirstOffset through actual log end
          persist workspace checkpoints before partition completion marker
          successful reconstruction -> add ID to deployable IDs
      - checkpoint-read, scan, or write failure -> add operation-wrapped error to map;
          omit failed ID; do not cancel siblings
      - wait for every scheduled attempt
      - setup failure before scheduling or context cancellation -> fatal error
      - return deployable IDs, partition-error map, fatal setup/context error or nil
  - log each returned map entry once with app QName and partition ID
  - fatal error -> stop bootstrap; do not deploy even partial ready results
  -> [DeployAppPartitions](app, deployable IDs), only if fatal is nil and IDs nonempty
  - continue bootstrap even when every partition is in the error map
```

### Deploy app partitions

```text
[VSQL parser]
  -> [AppDef builder] -> appdef.IAppDef
[Bootstrap operator]
  -> [DeployApp]: appdef.IAppDef, full declared partition count
       -> [Extension presence validator]
       - build extension engine pools per ProcessorKind; no partition runtimes
  -> [Checkpoint initial recovery]: wait for all scheduled partition attempts
       - partition errors -> log once; fatal error -> stop bootstrap
  -> [DeployAppPartitions]: only returned deployable IDs, if any
       - per supplied ID:
           create appPartitionRT
           deploy projector actualizers (async)
           deploy schedulers (async)
       - excluded IDs: no runtime partition, asynchronous projectors, or jobs
```

### Reject incompatible redeployment

```text
[Bootstrap operator]
  -> deployment descriptor validation:
       - cluster partition deployed:
           [c.cluster.DeployApp] -> [Deployment descriptor validator]
       - cluster partition excluded:
           [Deployment descriptor validator] directly, read only
       -> [cluster.App uniqueness check] -> [(cluster.App)]
       - either path: descriptor mismatch -> existing 409 Conflict, bootstrap fails
  -> [DeployApp]
       -> [Extension presence validator]
            VSQL-declared extension missing in code      -> panic, bootstrap fails
            built-in extension missing in VSQL           -> panic, bootstrap fails
```

## Cross-cutting concerns

### Bootstrap failure and readiness

- Every `[Checkpoint initial recovery]` invocation returns only after joining its scheduled attempts; a partition-scoped failure does not cancel sibling attempts. Every `[Bootstrap operator]` deployment call uses only eligible IDs from an invocation with a nil fatal error.
- Every partition-scoped initial-recovery error, including errors for the cluster partition and the all-partitions-failed case, is nonfatal to bootstrap and readiness. Server readiness does not assert that all declared partitions are deployed. Every setup error before scheduling and every bootstrap-context cancellation remains fatal.
- Every app registered by `[DeployApp]` retains its declared routing metadata independently of runtime partition deployment. Every excluded partition stays undeployed for the current run, even if storage becomes healthy; requests cannot retry initial recovery or deploy it. A later bootstrap can retry missing completion markers; in-process redeployment is outside this change.
- Every configured app follows compatibility validation even when the cluster partition is excluded. That branch performs no deployment-command retries or command-dependent provisioning, and may reach readiness with newly configured apps still registered but undeployed. Compatibility or read-only lookup errors are not suppressed as partition-attempt failures.
