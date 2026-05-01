# How: GDPR account deletion and data removal

## Approach

- Introduce a single registry-level orchestrator command `c.registry.InitiateDeleteAccount` in `pkg/registry` that owns account-deletion semantics; `cdoc.registry.Login` lives in `sys/registry`, so the entry point belongs there alongside `impl_changepassword.go` and `impl_resetpassword.go`
  - Routed at `sys/registry/pseudoProfileWSID` like `c.registry.CreateLogin`, authorized by the system token
  - Inside the command:
    - Locate `cdoc.registry.Login` via existing `GetCDocLogin` in `utils.go`, fail with `errLoginDoesNotExist` if missing
    - Set `cdoc.registry.Login.sys.IsActive = false` through `args.Intents.UpdateValue` (no field overwrite, no hash erase, no PII touch)
    - Capture `Login.WSID` (profile WSID) and `AppName` for the federation cascade below
    - Return a single command result field `Report` carrying a human-readable (indented) JSON document that summarizes every change scheduled or applied by the orchestrator (see `Result schema` below); full cascade completion still happens asynchronously, so the report describes what was scheduled, not what is finalized
- Result schema for `c.registry.InitiateDeleteAccount.Report` — a single indented JSON object with these top-level keys:
  - `records`: array of objects, each `{ "qname": "<pkg.Entity>", "id": <RecordID>, "wsid": <WSID>, "fields": { ... new values ... } }` covering the registry `Login` flip and every `Subject` / `JoinedWorkspace` / `WorkspaceDescriptor` / `ChildWorkspace` flip that the orchestrator issues
  - `views`: array of objects, each `{ "qname": "<pkg.View>", "key": { ... partition + clustering fields ... }, "op": "delete" }` covering the `view.registry.LoginIdx` row removed by the extended `projectorLoginIdx`
  - `deactivatedWorkspaces`: array of objects, each `{ "wsid": <WSID>, "wsName": "<name>", "kind": "profile" | "child" | "joined" }` listing every workspace whose `WorkspaceDescriptor.Status` was driven to `ToBeDeactivated` (profile + each `cdoc.sys.ChildWorkspace` in the profile WS) or whose `cdoc.sys.JoinedWorkspace` was deactivated (one entry per inviting workspace walked from the profile WS)
  - `notes`: optional array of short strings used to surface non-fatal observations (e.g. workspace already inactive, no `JoinedWorkspace` records found)
- Reuse existing deactivation cascades via federation calls issued from an async projector triggered by the `Login` deactivation event (mirrors the federation pattern in `pkg/sys/workspace/impl_deactivate.go`):
  - Call `c.sys.InitiateDeactivateWorkspace` on the profile WS — the existing chain in `impl_deactivate.go` already flips `WorkspaceDescriptor.Status` to `ToBeDeactivated`, the async deactivator drives `Status=Inactive`, and `cmdOnChildWorkspaceDeactivatedExec` cascades to all child workspaces (no new cascade code needed)
  - Iterate `cdoc.sys.JoinedWorkspace` records in the profile WS (via `q.sys.Collection`) and for each call `c.sys.DeactivateJoinedWorkspace` on the profile WS — the handler in `impl_applyinviteevents.go` already deactivates the inviting-side `cdoc.sys.Subject` and the profile-side `cdoc.sys.JoinedWorkspace`, so `view.sys.SubjectsIdx` is left in the state already handled by the recently merged re-invite fix (see `archive/2604/2604221416-fix-reinvite-after-removal`)
  - `cdoc.sys.Invite` records in the inviting workspaces stay where they are; deactivation of `Subject` is what unblocks re-invite, so no separate `Invite` walk is required for the critical requirements
- Make a deactivated login look non-existent to all login-resolution callers:
  - In `pkg/registry/utils.go` `GetCDocLogin`, treat `cdocLogin.AsBool(sys.IsActive) == false` as "not found" — a single change naturally fixes sign-in (`impl_issueprincipaltoken.go` already returns `errLoginOrPasswordIsIncorrect`), `impl_changepassword.go`, `impl_resetpassword.go`, and `UpdateGlobalRoles`, and lets a fresh `c.registry.CreateLogin` succeed for the same login string (existing duplicate check in `impl_createlogin.go` uses `GetCDocLoginID` against `view.registry.LoginIdx`, so the index must also be updated — see next bullet)
- Fix `view.registry.LoginIdx` so a re-sign-up under the same login produces a brand-new `cdoc.registry.Login`:
  - Extend `projectorLoginIdx` in `impl_createlogin.go` to remove the `(AppWSID, AppIDLoginHash)` view entry when the triggering CUD is an `IsActive=false` update of `cdoc.registry.Login` (matches the duplicate-removal pattern from the re-invite fix). This keeps the old, deactivated `Login` record intact for audit while freeing the lookup key
- Update `dbutils/dbutils_test.go` `TestGenerateAdminTokenByLogin` to fail fast when the resolved `cdoc.registry.Login.sys.IsActive == false`, returning an explicit "account is deleted" error before issuing a token (use the same `cdoc.registry.Login` lookup the test already performs to fetch `ProfileWSID`)
- Integration tests in `pkg/sys/it/impl_signupin_test.go` (or a new sibling file under `pkg/sys/it/`) covering the four critical guarantees from `issue.md`:
  - Re-sign-up under the same login yields an empty new profile (different `cdoc.registry.Login.sys.ID`, fresh profile WSID)
  - Re-invite of the new login into a workspace where the deleted login was previously invited succeeds and reaches `State_Joined` (relies on the existing re-invite fix combined with the `Subject` deactivation done here)
  - Sign-in via `api/v2/.../auth/login` for the deleted login returns the same "login or password is incorrect" response as for a never-created login
  - `TestGenerateAdminTokenByLogin`-equivalent path returns the new "account is deleted" error

References:

- [pkg/registry/impl_createlogin.go](../../../pkg/registry/impl_createlogin.go)
- [pkg/registry/impl_changepassword.go](../../../pkg/registry/impl_changepassword.go)
- [pkg/registry/impl_resetpassword.go](../../../pkg/registry/impl_resetpassword.go)
- [pkg/registry/impl_issueprincipaltoken.go](../../../pkg/registry/impl_issueprincipaltoken.go)
- [pkg/registry/utils.go](../../../pkg/registry/utils.go)
- [pkg/registry/appws.vsql](../../../pkg/registry/appws.vsql)
- [pkg/sys/workspace/impl_deactivate.go](../../../pkg/sys/workspace/impl_deactivate.go)
- [pkg/sys/invite/impl_deactivatejoinedworkspace.go](../../../pkg/sys/invite/impl_deactivatejoinedworkspace.go)
- [pkg/sys/invite/impl_applyinviteevents.go](../../../pkg/sys/invite/impl_applyinviteevents.go)
- [pkg/sys/invite/utils.go](../../../pkg/sys/invite/utils.go)
- [pkg/sys/sys.vsql](../../../pkg/sys/sys.vsql)
- [pkg/sys/it/impl_signupin_test.go](../../../pkg/sys/it/impl_signupin_test.go)
- [pkg/sys/it/impl_deactivateworkspace_test.go](../../../pkg/sys/it/impl_deactivateworkspace_test.go)
- [dbutils/dbutils_test.go](../../../dbutils/dbutils_test.go)
- [uspecs/changes/archive/2604/2604221416-fix-reinvite-after-removal/change.md](../archive/2604/2604221416-fix-reinvite-after-removal/change.md)
