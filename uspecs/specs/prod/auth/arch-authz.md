# Context subsystem architecture: prod/auth/authz

Authorization subsystem architecture covering runtime ACL evaluation, the enforcement points exposed to other contexts, and the four sources of effective roles composed for every request. Context-level overview and shared concepts: [arch.md](./arch.md). Identity payload contract consumed at validation time: [arch-tokens.md](./arch-tokens.md). Origin of the subjects doc and joined-workspace records consumed during composition: [arch-membership.md](./arch-membership.md).

## External actors

Roles:

- `@Client`
  - Caller whose request flows through `[Auth boundary]` at every authnz enforcement point in command, query, and actualizer processors.

Systems:

- `*apps`
  - The `apps` context (command, query, and actualizer processors) is the only programmatic consumer of `[Auth boundary]`; it calls `Authenticate` before invoking the ACL engine. See [../apps/arch.md](../apps/arch.md).

## Scenarios overview

- **`Authenticate request`**
  - `*apps` calls `[Auth boundary].Authenticate(ctx, app, appTokens, AuthnRequest{Host, RequestWSID, Token})`; the subsystem returns one authentication result containing PrincipalPayload and effective Principals, or an error

- **`Compose principals - anonymous`**
  - When the request has no token, the `Guest` user, the `Anonymous` role, the `Host` principal, and any `[(cdoc.sys.Subject)]` rows that match `sys.Guest` in `RequestWSID` are emitted.

- **`Compose principals - API token`**
  - When `PrincipalPayload.IsAPIToken=true`, principal composition uses only matching `RoleScopeWorkspace` grants plus `AuthenticatedUser`; no Host, no Subject, no GlobalRoles, no derived workspace roles, and no Workspace descriptor read

- **`Compose principals - user or device token`**
  - When `IsAPIToken=false`, the subsystem emits `AuthenticatedUser`, the `Host` principal, all four role sources, and the workspace-derived roles (`ProfileOwner` / `WorkspaceOwner` / `WorkspaceDevice`) appropriate to the `RequestWSID`.

## Components

### Layers

```text
External actors
    |
    +-- @Client (indirectly via *apps)
    +-- *apps
    |
    v
Boundary
    |
    +-- [Auth boundary] (IAuthenticator)
    |
    v
Composition
    |
    +-- [Token validator]
    +-- [Subjects reader]
    +-- [Workspace role deriver]
    +-- [Role inheritance map]
    |
    v
Role sources
    |
    +-- [Invite-granted]    (cdoc.sys.Subject in RequestWSID)
    +-- [Token-carried]     (PrincipalPayload.Roles + GlobalRoles)
    +-- [Request-context]   (Host, AuthenticatedUser, ProfileOwner, WorkspaceOwner, WorkspaceDevice, System)
    +-- [Anonymous-grants]  (Anonymous, Guest + sys.Guest subjects-doc rows)
```

### Boundary

- `[Auth boundary]`
  - Shared concept; see [arch.md#shared-concepts](./arch.md#shared-concepts). The single programmatic surface exposed to `*apps`; implemented by `implIAuthenticator.Authenticate`.
  - Effective Principals contain no WSID; the Application and RequestWSID stay in request context, and profile identity comes from [PrincipalPayload](./context.md#authnresult)
  - A successful result carries identity even when API-token or System composition omits User and Device Principals; failed authentication must not expose a partial result for authorization
  - AuthnResult contains PrincipalPayload (the original validated PrincipalPayload), Principals (effective request-local authorization inputs), and SubjectRoles (distinct roles read from Subjects in the current Workspace); composition leaves payload grants unchanged and deduplicates effective Principals by Kind, Name, QName, and ID

### Composition

- `[Token validator]`
  - When `Token` is non-empty, calls `[IAppTokens].ValidateToken(token, &PrincipalPayload)`, which returns role grants with validated, normalized scopes according to the [principal payload contract](./arch-tokens.md#payload-contract). Failures short-circuit composition with the validation error
  - impl: [pkg/iauthnzimpl/impl.go#Authenticate](../../../../pkg/iauthnzimpl/impl.go)

- `[Subjects reader]`
  - Reads Subject roles for the canonical Login and token-snapshotted Alias in RequestWSID, or sys.Guest for anonymous requests, and emits WSID-free Role Principals
  - Retains each distinct Subject role in AuthnResult.SubjectRoles independently of Principal deduplication; roles already supplied by other sources remain eligible for Subject-role enrichment
  - impl: [pkg/iauthnzimpl/impl.go#Authenticate](../../../../pkg/iauthnzimpl/impl.go)

- `[Workspace role deriver]`
  - Reads the workspace descriptor (`appdef.QNameCDocWorkspaceDescriptor`) for `RequestWSID`, compares its `OwnerWSID` against `ProfileWSID`, and emits `ProfileOwner` (same), `WorkspaceOwner` (user owner match), or `WorkspaceDevice` (device, per app's `isDeviceAllowed`); always emits `Host` (unless API token) and `AuthenticatedUser` (when token is present).
  - impl: [pkg/iauthnzimpl/impl.go#Authenticate](../../../../pkg/iauthnzimpl/impl.go)

- `[Role inheritance map]`
  - Static map (`ProfileOwner -> WorkspaceOwner`, `WorkspaceDevice -> WorkspaceOwner`, `RoleWorkspaceOwner -> WorkspaceOwner`) applied during ACL evaluation so that a single emitted role grants its parent role's privileges.
  - decl: [pkg/iauthnz/authn-types.go#rolesInheritance](../../../../pkg/iauthnz/authn-types.go)

### Role sources

Four sources by origin contribute to a request's principal set; each is enumerated below with the read path used by `[Workspace role deriver]`.

- `[Invite-granted]`
  - Origin: `[[Workspace membership]]` writes `[(cdoc.sys.Subject)]` rows in the inviting workspace as part of the join flow; see [arch-membership.md](./arch-membership.md).
  - Composition: reads roles for the canonical Login and token-snapshotted Alias in RequestWSID, adds effective Role Principals, and retains distinct role names in SubjectRoles

- `[Token-carried]`
  - Composition: regular tokens emit GlobalRoles and matching exact-Workspace or owned-by roles; API tokens emit matching exact-Workspace roles, with all source grants retained unchanged in PrincipalPayload
  - Origin: `[[Authentication]]` snapshots `GlobalRoles` at sign-in, including updates made through `[c.registry.UpdateGlobalRoles]`; see [arch-authn.md](./arch-authn.md). Token role grants are populated by trusted application-bound issuance, API-token issuance, and `[q.sys.EnrichPrincipalToken]`; their lifecycle and scope encoding are defined in [Token management](./arch-tokens.md)
  - API tokens process exact-Workspace grants before returning, while regular User/Device tokens process exact-Workspace and owned-by grants after their existing System short-circuits and descriptor-based role derivation. Exact matching does not require a Workspace descriptor; owned-by matching requires its OwnerWSID

- `[Request-context]`
  - Origin: computed at composition time from request state (`RequestWSID`, `Host`, token presence, `ProfileWSID`, workspace descriptor's `OwnerWSID`, app-supplied `isDeviceAllowed`), not from any persisted record.
  - Composition: `Host` (unless API token), `AuthenticatedUser` (when token is present), `ProfileOwner` (when `RequestWSID==ProfileWSID`), `WorkspaceOwner` (user, descriptor's `OwnerWSID==ProfileWSID`), `WorkspaceDevice` (device, `isDeviceAllowed` true), and the `System` short-circuit (when principals already include `role.sys.System` or when `ProfileWSID==NullWSID`).

- `[Anonymous-grants]`
  - Origin: emitted when no token is presented; combines the reserved Anonymous role and Guest User Principal with roles from Subject rows matching `sys.Guest`; Guest's ProfileWSID belongs to PrincipalPayload, not the Principal
  - Composition: `Anonymous` and `Guest` are added unconditionally on the no-token branch; the `[Subjects reader]` is invoked with the `sys.Guest` login so any guest-bound role grants persisted by `[[Workspace membership]]` apply.

Token-carried role matching:

| Role scope                   | Matching condition                                                             | Composition stage                |
| ---------------------------- | ------------------------------------------------------------------------------ | -------------------------------- |
| `RoleScopeWorkspace`         | `role.WSID == RequestWSID`                                                      | API or regular User/Device token |
| `RoleScopeWorkspacesOwnedBy` | The request Workspace descriptor exists and `role.WSID == descriptor.OwnerWSID` | Regular User/Device token        |

The [auth model](./context.md#rolescope) defines scope meanings. Missing scopes, explicit zero, and invalid scope claims are handled by the [principal payload validation contract](./arch-tokens.md#payload-contract) before composition. Matching grants emit WSID-free Role Principals, deduplicated against the other effective role sources; original grant scopes and Workspace bindings remain in PrincipalPayload.

An exact-Workspace grant limits only that grant's applicability. A regular token can still authorize requests elsewhere through its other role sources; API-token restricted composition remains controlled by `IsAPIToken`.

VSQL role inheritance (e.g., `ProfileOwner -> WorkspaceOwner`) is expanded later by the ACL engine, not during composition; see `[Role inheritance map]` above.

## Scenarios

### Authenticate request and compose principals

```text
*apps
  -> [Auth boundary].Authenticate(ctx, app, appTokens, request)
       -> if no bearer token:
            initialize Guest PrincipalPayload
            emit Guest User Principal, Anonymous, matching Guest Subject roles, and Host
            return AuthnResult
       -> [Token validator]: validate into result.PrincipalPayload
       -> emit AuthenticatedUser
       -> if API token: emit matching exact-Workspace roles and return
       -> emit GlobalRoles
       -> [Subjects reader]: emit roles matching canonical Login and Alias and retain their distinct names in SubjectRoles
       -> validate SubjectKind using the existing regular-token rules
       -> if System role exists: emit Host and return
       -> if PrincipalPayload.ProfileWSID is null: emit System and Host and return
       -> emit User or Device Principal
       -> [Workspace role deriver]: read Workspace descriptor and derive contextual roles
       -> emit exact token roles matching RequestWSID and owned-by roles matching the descriptor's OwnerWSID
       -> emit Host
       -> return AuthnResult containing original PrincipalPayload, effective Principals, and SubjectRoles
```

Device eligibility is evaluated in its existing stage even when GlobalRoles or Subject roles already supplied WorkspaceDevice. A successful check contributes the same effective Role value, which is deduplicated. Only a WorkspaceDevice role read from Subjects is retained in SubjectRoles for enrichment; GlobalRoles and eligibility alone do not create a new grant. Enrichment uses that snapshot without repeating eligibility checks or Subject reads.

### ACL evaluation by \*apps

```text
*apps (command/query processor)
  -> [Auth boundary].Authenticate -> authentication result
  -> check System role within this request's effective Principals
  -> evaluate rule.AllowedRoles vs effective Principals, applying [Role inheritance map]
  -> on deny: 403; on allow: proceed to extension
```

## Notes

The System short-circuits retain their existing positions after GlobalRoles and Subject-role lookup and before User/Device Principal emission, descriptor reads, and token-grant matching. PrincipalPayload remains available, and Host is still emitted for regular tokens. API-token composition returns earlier without Host, GlobalRoles, Subject reads, descriptor reads, or Workspace role derivation. Regular tokens also honor exact-Workspace grants so newly enriched roles apply only in their target Workspace; existing owned-by grants retain their meaning.


`Anonymous`, `Guest`, `Everyone`, and `AuthenticatedUser` come from `pkg/iauthnz/authn-types.go` and are reserved names; ACL rules in `apps` reference these QNames to express coverage levels.
