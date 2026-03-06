# Implementation plan: Use context-aware logging in actualizers

## Construction

- [x] update: [pkg/processors/utils.go](../../../pkg/processors/utils.go)
  - add: `CudOp(cud istructs.ICUDRow) string` — shared helper mapping `IsNew/IsActivated/IsDeactivated` to `"create"/"activate"/"deactivate"/"update"`

- [x] create: [pkg/processors/logging.go](../../../pkg/processors/logging.go)
  - add: `processors.LogEventAndCUDs(...)` — shared event/CUD logging skeleton with args JSON logging, event attrs, per-CUD attrs, shared `newfields=%s` logging, and one callback that decides whether to log a CUD and what extra message to append

- [x] update: [pkg/processors/command/impl.go](../../../pkg/processors/command/impl.go)
  - update: delegate common event/CUD logging to `processors.LogEventAndCUDs(...)` and keep command-specific `oldfields=%s` formatting local

- [x] update: [pkg/processors/actualizers/async.go](../../../pkg/processors/actualizers/async.go)
  - update: `asyncActualizer` — store base `logCtx` (vapp + extension) built in `init()`; pass it through `workpiece` so `keepReading` n10n trace uses it
  - update: `keepReading` — replace `logger.Trace`/`logger.TraceCtx` calls with `logger.IsVerbose`/`logger.VerboseCtx` using the base logCtx
  - update: `asyncProjector.DoAsync` — enrich base logCtx with `wsid` per-event; add `logEventAndCUDs` call before `Invoke`; log `msg=success` on success and `msg=failure` before returning error
  - update: `logEventAndCUDs(logCtx, event, pLogOffset, prj, appDef)` — delegate common event/CUD logging to `processors.LogEventAndCUDs(...)` and keep projector-specific CUD filtering local via the per-CUD callback

- [x] update: [pkg/processors/actualizers/async_test.go](../../../pkg/processors/actualizers/async_test.go)
  - update: existing tests that assert on log output or projector trigger behavior to match new log structure
