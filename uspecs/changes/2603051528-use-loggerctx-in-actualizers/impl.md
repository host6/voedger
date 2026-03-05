# Implementation plan: Use context-aware logging in actualizers

## Construction

- [ ] update: [pkg/processors/utils.go](../../../pkg/processors/utils.go)
  - add: `CudOp(cud istructs.ICUDRow) string` — shared helper mapping `IsNew/IsActivated/IsDeactivated` to `"create"/"activate"/"deactivate"/"update"`

- [ ] update: [pkg/processors/command/impl.go](../../../pkg/processors/command/impl.go)
  - update: replace local `cudOp` with `processors.CudOp`

- [ ] update: [pkg/processors/actualizers/async.go](../../../pkg/processors/actualizers/async.go)
  - update: `asyncActualizer` — store base `logCtx` (vapp + extension) built in `init()`; pass it through `workpiece` so `keepReading` n10n trace uses it
  - update: `keepReading` — replace `logger.Trace`/`logger.TraceCtx` calls with `logger.IsVerbose`/`logger.VerboseCtx` using the base logCtx
  - update: `asyncProjector.DoAsync` — enrich base logCtx with `wsid` per-event; add `logEventAndCUDs` call before `Invoke`; log `msg=success` on success and `msg=failure` before returning error
  - add: `logEventAndCUDs(logCtx, event, pLogOffset, prj, appDef)` — logs event args JSON and triggered CUDs (all CUDs for execute projectors; only triggered CUDs for insert/update projectors) using `processors.CudOp`

- [ ] update: [pkg/processors/actualizers/async_test.go](../../../pkg/processors/actualizers/async_test.go)
  - update: existing tests that assert on log output or projector trigger behavior to match new log structure

