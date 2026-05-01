# Implementation plan: Match vsql and code funcs definitions

## Construction

### Validator

- [x] update: [pkg/iextengine/builtin/impl.go](../../../pkg/iextengine/builtin/impl.go)
  - add: `AppFuncs()` and `StatelessFuncs()` accessors on the BuiltIn factory returning the existing per-app and stateless `BuiltInExtFuncs` maps for deployment-time validation
- [ ] Review
- [ ] update: [pkg/appparts/impl_app.go](../../../pkg/appparts/impl_app.go)
  - add: package-local structural interface `builtInFuncsRegistry { AppFuncs(); StatelessFuncs() }` so the BuiltIn factory is matched via Go duck typing without exposing a new public type in `pkg/iextengine`
  - add: `validateExtensions(def, eef, extModuleURLs)` function that, before pools are constructed in `appRT.deploy`:
    - iterates `appdef.Extensions(def.Types())` once and switches on `ext.Engine()`:
      - `BuiltIn`: requires the BuiltIn factory accessor (via the package-local interface) to report a matching `FullQName` for `a.name`; collects mismatches as "in vsql, not in code"
      - `WASM`: keeps current behaviour - names are accumulated into `ExtensionModule.ExtensionNames` and validated by `wazero.initModule`; surfaces that error as a typed deployment error rather than a panic
    - inverse pass: every BuiltIn entry whose `FullQName` belongs to `a.name` and was not visited during the AppDef walk is collected as "in code, not in vsql"
    - aggregates all mismatches via `errors.Join` and returns a single composite error listing each offending `FullQName`, kind (projector / command / query / job) and direction
  - update: `appRT.deploy` calls the validator first; on error, panics with the composite error (consistent with the existing `panic(err)` style in `DeployApp`)

### Cleanup of subsumed checks

- [ ] update: [pkg/istructsmem/appstruct-types.go](../../../pkg/istructsmem/appstruct-types.go)
  - remove: `validateResources()` and `validateJobs()` together with their two call sites in `AppConfigType.prepare()`
- [ ] update: [pkg/processors/actualizers/provide.go](../../../pkg/processors/actualizers/provide.go)
  - note: the unguarded `appdef.Projector(appStructs.AppDef().Type, projector.Name).Sync()` becomes unreachable for missing entries because deployment now fails first; no code change required, but verify the call site stays correct after the validator lands

### Tests

- [ ] update: [pkg/appparts/impl_test.go](../../../pkg/appparts/impl_test.go)
  - add: subtest `Test_DeployApp_validateExtensions`:
    - `vsql declares a builtin projector / command / query with no code implementation` -> `DeployApp` panics with composite error containing the offending FullQName and direction `in vsql, not in code`
    - `code registers a stateless projector / command / query absent from AppDef` -> same composite error, direction `in code, not in vsql`
    - `aligned set` -> `DeployApp` succeeds
    - `wasm-engine extension declared in vsql with name not exported by the wasm module` -> `DeployApp` surfaces a typed deployment error wrapping the wazero engine error
- [ ] update: [pkg/iextengine/builtin/impl_test.go](../../../pkg/iextengine/builtin/impl_test.go)
  - add: covers the new accessor for present / absent `(AppQName, FullQName)` in both per-app and stateless maps

- [ ] Review
