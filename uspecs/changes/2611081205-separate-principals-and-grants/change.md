---
change_id: 2611081205-separate-principals-and-grants
type: fix
issue_url: https://untill.atlassian.net/browse/AIR-5213
domains: [prod]
scope: [auth, apps]
breaking: true
---

# Change request: Request-scoped principals and token enrichment

Refs:

- [AIR-5213: Remove Principal.WSID and separate effective principals from token grants](./issue-AIR-5213.md)

## Why

Principals mix request Workspace applicability with Subject profile identity. Returning the original token payload separately preserves identity and original grants, while a uniform enrichment binding removes the need to reconstruct role origins.

## What

- AuthnResult contains exactly PrincipalPayload, the original validated PrincipalPayload, and Principals, the effective authorization inputs for the current request
- Effective Principals contain no WSID; request Application and Workspace remain in request context, and Subject identity comes from PrincipalPayload
- Enrichment preserves original grants and adds each effective role using the request Workspace, with owned-by scope for regular tokens and exact-Workspace scope for API tokens
- The accepted policy changes newly enriched WorkspaceDevice grants from the Device ProfileWorkspace binding to the request Workspace; existing carried grants retain their original bindings
- Original token authorization decisions, scope matching, early returns, endpoint permissions, token identity claims, refresh behavior, and enrichment lifetime remain unchanged
- RequestSubject and identity logging obtain canonical Login, SubjectKind, and ProfileWSID even when API-token or System composition omits User or Device Principals

## How

Decisions:

- Keep AuthnResult limited to PrincipalPayload and Principals; use the existing PrincipalPayload type for state identity callbacks and test fixtures
- Authentication retains the original validated payload, including normalized legacy scopes, and never prepares additional grants in it
- Synthetic Guest payloads represent anonymous requests and anonymous-only function shortcuts without implying bearer-token validation
- Enrichment copies PrincipalPayload and clones its Roles slice before appending complete grant tuples, preserving the result and any shared backing slice
- Use requestWSID for every proposed grant independently of role origin; ACL inheritance and legacy ACL expansion do not independently produce grants
- Preserve explicit parent-to-child propagation: repeated enrichment within one Workspace is idempotent, while enrichment in an authorized child Workspace may extend access to its directly owned Workspaces
- Evaluate Device eligibility in its existing composition stage even when another source already supplied WorkspaceDevice; deduplicate effective Principals without recording sources
- Migrate state callbacks, command/query processors, N10n, System checks, legacy Guest matching, and request logging together

Acceptance criteria for the changed binding:

- Given a Device profile `100` making an authorized enrichment request in Workspace `200`, newly captured WorkspaceDevice uses `{Scope: WorkspacesOwnedBy, WSID: 200, QName: sys.WorkspaceDevice}`
- If GlobalRoles already supplies WorkspaceDevice, both successful and unsuccessful Device eligibility checks produce that same new binding without rereading eligibility during enrichment
- If no source grants WorkspaceDevice and Device eligibility fails, enrichment does not add WorkspaceDevice
- Any previously carried WorkspaceDevice grant bound to `100` remains unchanged
- API-token additions use `{Scope: Workspace, WSID: requestWSID, QName: role}` and never introduce owned-by scope

Assumptions:

- None

Out of scope:

- Restricting delegation depth or changing supported token-kind and scope combinations
- Adding source metadata, enrichment candidate types, or extra result fields
- Reworking ACL inheritance or legacy authorization policies

References:

- [Authentication boundary](../../../pkg/iauthnz/authn-interface.go)
- [Authentication composition](../../../pkg/iauthnzimpl/impl.go)
- [Token enrichment](../../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
- [Auth model and enrichment rules](../../specs/prod/auth/context.md)

## Domain design

- [x] update: [prod/domain.md](../../specs/prod/domain.md)
  - distinguish: request-scoped Principal authorization from Subject identity and token-carried RoleGrant values

- [x] update: [prod/auth/context.md](../../specs/prod/auth/context.md)
  - define: the two-field AuthnResult and PrincipalPayload identity contract, including synthetic Guest identity and early-return availability
  - define: uniform grant construction, original-grant preservation, tuple equality, the accepted WorkspaceDevice change, and repeated parent-to-child enrichment

## Technical design

- [x] update: [prod/auth/arch.md](../../specs/prod/auth/arch.md) and [prod/auth/arch-authz.md](../../specs/prod/auth/arch-authz.md)
  - document: original PrincipalPayload retention, WSID-free effective Principals, unchanged source composition stages, Device eligibility, and atomic failure results

- [x] update: [prod/auth/arch-tokens.md](../../specs/prod/auth/arch-tokens.md)
  - document: copying the original payload and adding effective roles at the request Workspace without source reconstruction or payload mutation

- [x] update: [prod/apps/arch-processing.md](../../specs/prod/apps/arch-processing.md)
  - document: result propagation, request isolation, PrincipalPayload identity projection, System and Guest checks, diagnostics, and verification ownership

## Construction

### Tests

- [x] update: [pkg/iauthnz/utils_test.go](../../../pkg/iauthnz/utils_test.go)
  - verify: System role checks without Principal.WSID

- [x] update: [pkg/iauthnzimpl/impl_test.go](../../../pkg/iauthnzimpl/impl_test.go)
  - verify: original PrincipalPayload preservation, User/Device/Guest/API/System identity, legacy scopes, effective-role deduplication, Device eligibility, and empty results on failure

- [x] update: [sys/storages/impl_subject_storage_test.go](../../../pkg/sys/storages/impl_subject_storage_test.go)
  - verify: PrincipalPayload identity projection and per-request callback freshness without Principal scanning

- [x] update: [state/stateprovide/impl_host_state_test.go](../../../pkg/state/stateprovide/impl_host_state_test.go)
  - verify: command state PrincipalPayload callbacks with request Workspace different from ProfileWSID

- [x] update: [state/stateprovide/impl_query_processor_state_test.go](../../../pkg/state/stateprovide/impl_query_processor_state_test.go)
  - verify: query state PrincipalPayload callbacks and unchanged Token retrieval

- [x] update: [pkg/processors/utils_test.go](../../../pkg/processors/utils_test.go)
  - verify: anonymous-only result replacement and synthetic Guest PrincipalPayload

- [x] update: [processors/command/impl_test.go](../../../pkg/processors/command/impl_test.go)
  - verify: authentication-result and Principal accessors and result clearing on release in one set/read/release cycle

- [x] update: [processors/query/impl_test.go](../../../pkg/processors/query/impl_test.go)
  - verify: extension-visible original PrincipalPayload and request identity, failed authentication, sequential requests, and release
  - distinguish: failed authentication prevents extension execution; this integration test does not directly observe internal failure cleanup or workpiece reuse

- [x] update: [processors/query2/impl_test.go](../../../pkg/processors/query2/impl_test.go)
  - verify: result release and refresh routing/response ProfileWSID sourced from PrincipalPayload
  - reuse: the production implIQueryMessage for refresh requests instead of a local message double

- [x] update: [processors/n10n/impl_test.go](../../../pkg/processors/n10n/impl_test.go)
  - verify: per-Workspace subscription authorization and existing unauthorized/forbidden mapping
  - reuse: coreutils.MockAppStructs, MockRecords, and TestObject for subscription Workspace descriptors

- [x] update: [processors/oldacl/impl_test.go](../../../pkg/processors/oldacl/impl_test.go)
  - verify: reserved Guest identity matching, authorization role ancestry, Verbose denial diagnostics, and separate Workspace/profile identifiers
  - verify: explicit legacy role enrichment and unchanged input independently of authorization ancestry lookup
  - remove: token-exclusion assertions without token input and duplicated System-principal checks covered in iauthnz

- [x] update: [sys/authnz/impl_enrichprincipaltoken_test.go](../../../pkg/sys/authnz/impl_enrichprincipaltoken_test.go)
  - verify: explicit expected grants for both token kinds, original/unmatched grant retention, duplicate-role exclusion, shared-slice immutability, and idempotent repeated enrichment
  - verify: GlobalRole-backed Device eligibility allowed/denied and derived-only roles retain request Workspace binding without a second eligibility check
  - verify: a missing bearer token returns ErrInvalidToken without invoking the result callback
  - reuse: coreutils state, key, value, app, and records mocks with explicit expectations and TestObject descriptors; keep the workpiece double local to avoid an import cycle
  - remove: redundant assertions and revalidation of the unchanged original input token; retain tests that execute production enrichment and authentication

- [x] update: [itokens-payloads/impl_test.go](../../../pkg/itokens-payloads/impl_test.go)
  - verify: explicit scope round trips, legacy scope normalization for both token kinds, and rejection of null, malformed, or unknown scope values

- [x] update: [sys/it/impl_childworkspace_test.go](../../../pkg/sys/it/impl_childworkspace_test.go)
  - verify: existing authorization, full next-level grant tuples, repeated enrichment, identity preservation, refresh, and API-token isolation

- [x] update: [tests/wasm/main_test.go](../../../pkg/iextengine/wazero/_testdata/tests/wasm/main_test.go)
  - verify: extension-visible SubjectKind, Login, ProfileWSID, and Token through the PrincipalPayload test-state fixture

### Authentication and consumers

- [x] update: [pkg/coreutils/mock.go](../../../pkg/coreutils/mock.go)
  - implement: shared MockAppStructs and MockRecords with a nil-safe result helper for reused app and record expectations

- [x] update: [itokens-payloads/consts.go](../../../pkg/itokens-payloads/consts.go) and [itokens-payloads/types.go](../../../pkg/itokens-payloads/types.go)
  - implement: RoleScopeWorkspacesOwnedBy and RoleScopeWorkspace and explicit Scope in RoleType

- [x] create: [itokens-payloads/impl_principalpayload.go](../../../pkg/itokens-payloads/impl_principalpayload.go)
  - define: raw role scope claims used to distinguish missing claims from explicit values
  - implement: ValidateTokenClaims distinguishes absent legacy scope from explicit zero and rejects invalid scope claims

- [x] update: [pkg/itokensjwt/impl.go](../../../pkg/itokensjwt/impl.go)
  - implement: invoke optional payload claim validation after successful JWT verification and payload decoding

- [x] update: [pkg/iauthnzimpl/utils.go](../../../pkg/iauthnzimpl/utils.go)
  - implement: clone existing grants and bind carried and newly issued API-token grants to exact-Workspace scope

- [x] update: [pkg/iauthnz/authn-types.go](../../../pkg/iauthnz/authn-types.go)
  - implement: remove Principal.WSID and add AuthnResult with exactly PrincipalPayload and Principals

- [x] update: [pkg/iauthnz/authn-interface.go](../../../pkg/iauthnz/authn-interface.go)
  - implement: return AuthnResult and error from Authenticate

- [x] update: [pkg/iauthnz/utils.go](../../../pkg/iauthnz/utils.go)
  - implement: use request-local System checks and shared synthetic Guest payload construction

- [x] update: [pkg/iauthnzimpl/impl.go](../../../pkg/iauthnzimpl/impl.go)
  - implement: return original PrincipalPayload and deduplicated Principals while preserving composition order and clearing failed results

- [x] update: [pkg/state/types.go](../../../pkg/state/types.go)
  - implement: replace Principal callbacks with PrincipalPayload callbacks for command/query state

- [x] update: [state/stateprovide/impl_command_processor_state.go](../../../pkg/state/stateprovide/impl_command_processor_state.go)
  - implement: wire the PrincipalPayload provider into RequestSubject

- [x] update: [state/stateprovide/impl_query_processor_state.go](../../../pkg/state/stateprovide/impl_query_processor_state.go)
  - implement: wire the PrincipalPayload provider into RequestSubject

- [x] update: [sys/storages/impl_subject_storage.go](../../../pkg/sys/storages/impl_subject_storage.go)
  - implement: project identity from original PrincipalPayload while retaining storage fields and Token callback

- [x] update: [state/teststate/impl.go](../../../pkg/state/teststate/impl.go)
  - implement: accept explicit PrincipalPayload fixtures for RequestSubject and wire state providers

- [x] update: [state/teststate/interface.go](../../../pkg/state/teststate/interface.go)
  - implement: update PutRequestSubject to accept PrincipalPayload

- [x] update: [pkg/processors/types.go](../../../pkg/processors/types.go)
  - implement: expose the shared result through the processor workpiece

- [x] update: [pkg/processors/utils.go](../../../pkg/processors/utils.go)
  - implement: initialize anonymous-only results without adding grants

- [x] update: [processors/command/impl.go](../../../pkg/processors/command/impl.go)
  - implement: propagate and reset results, migrate System checks, and preserve legacy ACL decisions and log context

- [x] update: [processors/command/types.go](../../../pkg/processors/command/types.go)
  - implement: retain the result and wire its PrincipalPayload into reusable state

- [x] update: [processors/query/impl.go](../../../pkg/processors/query/impl.go)
  - implement: propagate and reset the result across query v1 authorization and extension execution

- [x] update: [processors/query2/impl.go](../../../pkg/processors/query2/impl.go)
  - implement: propagate PrincipalPayload and Principals through query v2 authorization and state construction

- [x] update: [query2/impl_auth_refresh_handler.go](../../../pkg/processors/query2/impl_auth_refresh_handler.go)
  - implement: route refresh requests and populate response ProfileWSID from the authentication result's PrincipalPayload

- [x] update: [query2/impl_cdocs_handler.go](../../../pkg/processors/query2/impl_cdocs_handler.go), [query2/impl_docs_handler.go](../../../pkg/processors/query2/impl_docs_handler.go), and [query2/impl_view_handler.go](../../../pkg/processors/query2/impl_view_handler.go)
  - implement: pass explicit request context and PrincipalPayload to legacy result authorization and retain profile context in fallback diagnostics

- [x] update: [processors/query2/util.go](../../../pkg/processors/query2/util.go)
  - implement: retain and reset the query v2 result and expose its fields

- [x] update: [processors/n10n/impl_subscribeandwatch.go](../../../pkg/processors/n10n/impl_subscribeandwatch.go)
  - implement: consume each subscription Workspace result independently

- [x] update: [processors/oldacl/acl.go](../../../pkg/processors/oldacl/acl.go)
  - implement: use explicit PrincipalPayload identity and request log context for Guest matching and diagnostics

- [x] update: [processors/oldacl/impl.go](../../../pkg/processors/oldacl/impl.go)
  - implement: remove Principal WSID matching and preserve reserved Guest identity semantics

- [x] update: [processors/oldacl/utils.go](../../../pkg/processors/oldacl/utils.go)
  - implement: remove WSID-bearing role expansion and Principal formatting

- [x] update: [sys/authnz/impl_enrichprincipaltoken.go](../../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
  - implement: clone original payload roles and append effective role grants using requestWSID and token-kind scope

### Validation

- [x] verify: scoped regressions and contract migration
  - run: focused tests for iauthnz, iauthnzimpl, authnz, storages, state providers, processors, command/query v1/v2, oldacl, and N10n
  - run: `go test ./pkg/sys/it -run '^TestForeignAuthorization$' -count=1`
  - run: `go -C pkg/iextengine/wazero/_testdata test ./tests/wasm -run '^Test_CommandStorages$' -count=1`
  - run: `go test ./... -run '^$'` to compile all root-module packages and tests without executing unrelated suites
  - run: scoped golangci-lint and `git diff --check`
  - review: original PrincipalPayload immutability, uniform WorkspaceDevice binding, unchanged token grant scopes, and request isolation
  - previous validation: scoped unit tests, TestForeignAuthorization, nested WASM Test_CommandStorages, root-module compilation, scoped lint, formatting, relative file links, and whitespace checks passed before the latest test refactors

- [x] review: latest test refactors
  - verify: root assertion initialization, independent expected grants, shared mock expectations, removed vacuous checks, and retained production behavior coverage
  - run: focused package regressions and documentation link, changed-file coverage, and whitespace checks during synchronization
  - result: tests pass for coreutils, iauthnz, iauthnzimpl, itokens-payloads, itokensjwt, processors, command, query, query2, oldacl, N10n, stateprovide, authnz, and storages; teststate compiles with no test files
  - result: every changed source and specification file outside change folders is linked in the plan, relative file links resolve, and git diff --check passes
