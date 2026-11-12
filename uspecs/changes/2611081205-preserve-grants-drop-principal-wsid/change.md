---
change_id: 2611081205-preserve-grants-drop-principal-wsid
type: fix
issue_url: https://untill.atlassian.net/browse/AIR-5213
domains: [prod]
scope: [auth, apps]
breaking: true
---

# Change request: Preserve initial grants on token enrichment + drop Principal.WSID

Refs:

- [AIR-5213: Remove Principal.WSID and separate effective principals from token grants](./issue-AIR-5213.md)

## Why

Token enrichment must preserve the initial grants, including their scope and Workspace binding, when adding roles from Subjects in the request Workspace. `Principal.WSID` cannot describe both: for roles it denotes the request Workspace, while for Users and Devices it denotes the Subject's profile Workspace. The original token payload must remain available separately so enrichment does not reconstruct or replace initial grants from effective Principals. Subject roles must also remain distinguishable from other role sources.

For example:

- Workspace `100` owns Workspace `200`, which owns Workspace `300`
- A regular token contains `{Scope: WorkspacesOwnedBy, WSID: 100, QName: WorkspaceOwner}`, granting access to Workspace `200`
- Authentication in Workspace `200` emits an effective `WorkspaceOwner` Principal; its request Workspace does not identify the initial grant's binding to `100`
- The previous enrichment added `{Scope: WorkspacesOwnedBy, WSID: 200, QName: WorkspaceOwner}`, unintentionally allowing access to Workspace `300`
- Enrichment must retain the initial grant without copying a role supplied only by the token into another grant
- If a Subject in Workspace `200` also grants WorkspaceOwner, enrichment adds `{Scope: Workspace, WSID: 200, QName: WorkspaceOwner}`, which applies only in Workspace `200`
- The initial owned-by grant bound to `100` must remain unchanged alongside any new Subject grant; rebuilding grants from effective Principals alone cannot recover the initial binding or identify a Subject assignment

This change preserves initial access while preventing added grants from extending access to other Workspaces. It keeps initial grants separately from effective Principals and drops `Principal.WSID`, using request context for the target Workspace and the original payload for Subject identity.

## What

- AuthnResult contains PrincipalPayload, the original validated claims; Principals, effective authorization inputs; and SubjectRoles, distinct roles read from Subjects in the current Workspace
- Effective Principals contain no WSID; request Application and Workspace remain in request context, and Subject identity comes from PrincipalPayload
- Enrichment preserves original grants and adds only Subject roles with exact-Workspace scope using the request Workspace
- GlobalRoles, token grants, built-in roles, ownership, and Device eligibility alone do not create additions; API-token enrichment has no additions because authentication skips Subjects
- A WorkspaceDevice role is captured only when a Subject grants it, using the request Workspace rather than the Device profile; existing carried grants retain their original bindings
- Regular-token authentication honors exact-Workspace grants as well as existing owned-by grants; API-token composition, early returns, endpoint permissions, token identity claims, refresh behavior, and enrichment lifetime remain unchanged
- RequestSubject and identity logging obtain canonical Login, SubjectKind, and ProfileWSID even when API-token or System composition omits User or Device Principals

## How

Decisions:

- Retain PrincipalPayload, Principals, and a SubjectRoles snapshot in AuthnResult; use the existing PrincipalPayload type for state identity callbacks and test fixtures
- Authentication retains the original validated payload, including normalized legacy scopes, and never prepares additional grants in it
- Synthetic Guest payloads represent anonymous requests and anonymous-only function shortcuts without implying bearer-token validation
- Enrichment copies PrincipalPayload and clones its Roles slice before appending complete grant tuples, preserving the result and any shared backing slice
- Record distinct Subject roles during canonical Login and Alias lookups, even when another source already supplied the same effective Principal; use requestWSID for their new exact grants
- Do not reconstruct role sources from effective Principals or reread Subjects during enrichment; ACL inheritance and legacy ACL expansion do not independently produce grants
- Match regular-token exact grants against requestWSID, independently of the Workspace descriptor; retain descriptor-based matching for owned-by grants
- Repeated enrichment within one Workspace is idempotent; added grants authorize only that Workspace, while initial grants retain their original applicability
- Evaluate Device eligibility in its existing composition stage even when another source already supplied WorkspaceDevice; deduplicate effective Principals without recording sources
- Migrate state callbacks, command/query processors, N10n, System checks, legacy Guest matching, and request logging together

Acceptance criteria for the changed binding:

- Given a Device profile `100` making an authorized enrichment request in Workspace `200`, a WorkspaceDevice role read from its Subject produces `{Scope: Workspace, WSID: 200, QName: sys.WorkspaceDevice}`
- If GlobalRoles or Device eligibility also supplies WorkspaceDevice, a Subject assignment still produces that exact grant without rereading eligibility or Subjects during enrichment
- If no Subject grants WorkspaceDevice, enrichment does not add it even when it is effective through GlobalRoles or Device eligibility
- Any previously carried WorkspaceDevice grant bound to `100` remains unchanged
- New Subject-role grants use `{Scope: Workspace, WSID: requestWSID, QName: role}` and never introduce owned-by scope; API tokens retain their existing grants without additions
- A VIT integration test removes the source Subject role after enrichment and proves that the token still permits writes in the target Workspace, denies writes in its owner, child, and sibling, and preserves initial read access to the sibling through refresh

Assumptions:

- None

Out of scope:

- Restricting the entire returned token to one Workspace by removing initial grants or changing ordinary GlobalRoles, Subject, ownership, or Device role derivation
- Tagging effective Principals with source metadata or introducing separate enrichment candidate types
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
  - define: AuthnResult with original PrincipalPayload, effective Principals, and SubjectRoles, including synthetic Guest identity and early-return availability
  - define: exact request-Workspace additions only from Subjects, API-token preservation without additions, tuple equality, WorkspaceDevice source filtering, and repeated enrichment

## Functional design

- [x] create: [auth/token-enrichment.feature](../../specs/prod/auth/token-enrichment.feature)
  - Feature specification derived from enrichment unit tests and TestForeignAuthorization, retaining one dedicated Rule and Scenario for request-Workspace binding without Principal.WSID and covering initial-grant preservation, Subject-only additions, unchanged API-token payloads, repeated regular-token enrichment, Device source filtering, Workspace isolation, and missing bearer tokens
  - trace: claim and grant preservation, canonical and Alias Subject roles, API-token non-expansion, issued Application and lifetime, and repeated enrichment to TestEnrichmentPreservesInitialGrantsAndAddsOnlySubjectRoles
  - trace: Device roles captured only from Subjects, including roles shared with GlobalRoles and eligibility, to TestDeviceRolesAreCapturedOnlyFromSubjects
  - trace: retained original access, target-only added roles after Subject deactivation, and repeated requests to TestForeignAuthorization
  - trace: missing-bearer-token rejection without an issued token to TestEnrichmentRejectsMissingBearerToken; this executor-level error scenario does not specify an HTTP status

- [x] update: [auth/authn.feature](../../specs/prod/auth/authn.feature)
  - merge: refresh scenarios into one Scenario Outline for tokens with no grants or with owned-by and exact Workspace grants
  - verify: complete PrincipalPayload, Application, and issued lifetime preservation, along with refresh response fields and Content-Type, in TestRefreshPrincipalToken
  - trace: grant applicability after refresh to TestForeignAuthorization

## Technical design

- [x] update: [prod/auth/arch.md](../../specs/prod/auth/arch.md) and [prod/auth/arch-authz.md](../../specs/prod/auth/arch-authz.md)
  - document: original PrincipalPayload retention, WSID-free effective Principals, SubjectRoles capture, regular-token exact-grant matching, existing source composition stages, Device eligibility, and atomic failure results

- [x] update: [prod/auth/arch-tokens.md](../../specs/prod/auth/arch-tokens.md)
  - document: copying the original payload and adding exact grants only from SubjectRoles without source reconstruction or payload mutation; API-token non-expansion; require scope-aware validators and scope-preserving refresh

- [x] update: [prod/auth/authn--td.md](../../specs/prod/auth/authn--td.md)
  - document: principal-token refresh preserves the complete PrincipalPayload, including RoleGrants, along with Application and issued lifetime

- [x] update: [prod/apps/arch-processing.md](../../specs/prod/apps/arch-processing.md)
  - document: result propagation, request isolation, PrincipalPayload identity projection, System and Guest checks, diagnostics, and verification ownership

## Construction

### Tests

- [x] update: [pkg/iauthnz/utils_test.go](../../../pkg/iauthnz/utils_test.go)
  - verify: System role checks without Principal.WSID

- [x] update: [pkg/iauthnzimpl/impl_test.go](../../../pkg/iauthnzimpl/impl_test.go)
  - verify: original PrincipalPayload preservation, User/Device/Guest/API/System identity, legacy scopes, effective-role deduplication, Device eligibility, and empty results on failure
  - reuse: merged MockAppStructs and MockRecords with descriptor fixtures; remove unused local app, record, view, and key implementations and unrelated fixture records
  - verify: source-specific Global, Subject, and token roles detect skipped composition stages while shared roles remain deduplicated; normalized legacy grants also produce effective Principals
  - verify: regular-token exact grants match only the request Workspace even without a descriptor, and coexist with owned-by grants for User and Device tokens
  - verify: SubjectRoles retains distinct canonical and Alias assignments shared with other role sources, remains empty for API tokens, and is discarded with partial results on lookup failure

- [x] remove: `pkg/sys/storages/impl_subject_storage_test.go`
  - consolidate: identity projection, storage key equality, and callback freshness coverage into TestCommandStateRequestSubject

- [x] update: [state/stateprovide/impl_host_state_test.go](../../../pkg/state/stateprovide/impl_host_state_test.go)
  - verify: command state PrincipalPayload callbacks with request Workspace different from ProfileWSID, key equality, Token retrieval, and fresh callback values without changing prior results

- [x] update: [pkg/processors/utils_test.go](../../../pkg/processors/utils_test.go)
  - verify: anonymous-only result replacement and synthetic Guest PrincipalPayload

- [x] update: [processors/query/impl_test.go](../../../pkg/processors/query/impl_test.go)
  - verify: TestAuthnResultAvailableInQuery exposes authenticated Login, SubjectKind, ProfileWSID, and the original Token through RequestSubject to a real query extension without User or Device Principals
  - consolidate: retain one request through the processor pipeline; this test does not directly verify failure cleanup, sequential requests, or workpiece release

- [x] update: [processors/n10n/impl_test.go](../../../pkg/processors/n10n/impl_test.go)
  - verify: per-Workspace subscription authorization and existing unauthorized/forbidden mapping
  - reuse: coreutils.MockAppStructs, MockRecords, and TestObject for subscription Workspace descriptors

- [x] update: [processors/oldacl/impl_test.go](../../../pkg/processors/oldacl/impl_test.go)
  - verify: reserved Guest identity matching, authorization role ancestry, and Verbose denial diagnostics with suppression below Verbose severity
  - verify: explicit legacy role enrichment and unchanged input independently of authorization ancestry lookup
  - remove: token-exclusion assertions without token input and duplicated System-principal checks covered in iauthnz

- [x] update: [sys/authnz/impl_enrichprincipaltoken_test.go](../../../pkg/sys/authnz/impl_enrichprincipaltoken_test.go)
  - verify: Subject-only exact additions for User and Device tokens, unchanged API-token grants, original/unmatched grant retention, duplicate-role exclusion, shared-slice immutability, and idempotent repeated enrichment
  - verify: GlobalRoles and Device eligibility alone produce no additions, while overlapping Subject assignments still produce exact grants without a second eligibility check
  - verify: a missing bearer token returns ErrInvalidToken without invoking the result callback
  - reuse: coreutils state, key, value, app, and records mocks with explicit expectations and TestObject descriptors; keep the workpiece double local to avoid an import cycle
  - remove: redundant assertions and revalidation of the unchanged original input token; retain tests that execute production enrichment and authentication

- [x] update: [itokens-payloads/impl_test.go](../../../pkg/itokens-payloads/impl_test.go)
  - verify: explicit scope round trips, legacy scope normalization for both token kinds, and rejection of null, malformed, or unknown scope values

- [x] update: [sys/it/impl_authn_feature_test.go](../../../pkg/sys/it/impl_authn_feature_test.go)
  - consolidate: principal-token refresh into TestRefreshPrincipalToken with no-grants and mixed-scope grant cases using shared request and assertion logic
  - verify: full payload, Application, issued lifetime, response ProfileWSID, expiration, Content-Type, and a newly issued token through the real refresh API

- [x] update: [sys/it/impl_childworkspace_test.go](../../../pkg/sys/it/impl_childworkspace_test.go)
  - verify: exact added grant tuples, writes in the target after source Subject removal, denied writes in its owner, child, and sibling, retained initial read access to its sibling, repeated enrichment, identity preservation, refresh, and API-token isolation

- [x] update: [tests/wasm/main_test.go](../../../pkg/iextengine/wazero/_testdata/tests/wasm/main_test.go)
  - verify: extension-visible SubjectKind, Login, ProfileWSID, and Token through the PrincipalPayload test-state fixture

### Authentication and consumers

- [x] update: [pkg/coreutils/mock.go](../../../pkg/coreutils/mock.go)
  - extend: the merged MockAppStructs with ClusterAppID and nil-safe AppTokens expectations, retaining its existing IAppStructs compliance check in coreutils/mock_test.go
  - reuse: shared MockRecords and other interface mocks supplied by AIR-5221 instead of duplicate local implementations

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
  - implement: remove Principal.WSID and add AuthnResult with PrincipalPayload, Principals, and distinct SubjectRoles

- [x] update: [pkg/iauthnz/authn-interface.go](../../../pkg/iauthnz/authn-interface.go)
  - implement: return AuthnResult and error from Authenticate
  - remove: unused NewDefaultAuthenticatorType and its obsolete signature comments

- [x] update: [pkg/iauthnz/utils.go](../../../pkg/iauthnz/utils.go)
  - implement: use request-local System checks and shared synthetic Guest payload construction

- [x] update: [pkg/iauthnzimpl/impl.go](../../../pkg/iauthnzimpl/impl.go)
  - implement: return original PrincipalPayload and deduplicated Principals while preserving composition order and clearing failed results
  - implement: match exact-Workspace grants for regular tokens in addition to existing owned-by matching
  - implement: retain distinct Subject roles independently of effective Principal deduplication using existing canonical Login and Alias lookups

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
  - implement: expose the shared result through GetAuthnResult and remove the unused GetPrincipals accessor

- [x] update: [pkg/processors/utils.go](../../../pkg/processors/utils.go)
  - implement: initialize anonymous-only results without adding grants

- [x] update: [processors/command/impl.go](../../../pkg/processors/command/impl.go)
  - implement: propagate and reset results, migrate System checks, and preserve legacy ACL decisions
  - remove: unused GetPrincipals method; retain GetAuthnResult for consumers
  - preserve: Verbose fallback-success logging without adding request context

- [x] update: [processors/command/types.go](../../../pkg/processors/command/types.go)
  - implement: retain the result and wire its PrincipalPayload into reusable state

- [x] update: [processors/query/impl.go](../../../pkg/processors/query/impl.go)
  - implement: propagate and reset the result across query v1 authorization and extension execution
  - remove: unused GetPrincipals method

- [x] update: [processors/query2/impl.go](../../../pkg/processors/query2/impl.go)
  - implement: propagate PrincipalPayload and Principals through query v2 authorization and state construction

- [x] update: [query2/impl_auth_refresh_handler.go](../../../pkg/processors/query2/impl_auth_refresh_handler.go)
  - implement: route refresh requests and populate response ProfileWSID from the authentication result's PrincipalPayload

- [x] update: [query2/impl_cdocs_handler.go](../../../pkg/processors/query2/impl_cdocs_handler.go), [query2/impl_docs_handler.go](../../../pkg/processors/query2/impl_docs_handler.go), and [query2/impl_view_handler.go](../../../pkg/processors/query2/impl_view_handler.go)
  - implement: pass effective Principals and PrincipalPayload to legacy result authorization; retain existing Verbose fallback diagnostics

- [x] update: [processors/query2/util.go](../../../pkg/processors/query2/util.go)
  - implement: retain and reset the query v2 result and expose it through GetAuthnResult
  - remove: unused GetPrincipals method and separate profileWSID field

- [x] update: [processors/n10n/impl_subscribeandwatch.go](../../../pkg/processors/n10n/impl_subscribeandwatch.go)
  - implement: consume each subscription Workspace result independently

- [x] update: [processors/oldacl/acl.go](../../../pkg/processors/oldacl/acl.go)
  - implement: use explicit PrincipalPayload identity for Guest matching; retain Verbose denial diagnostics without adding a context argument

- [x] update: [processors/oldacl/impl.go](../../../pkg/processors/oldacl/impl.go)
  - implement: remove Principal WSID matching and preserve reserved Guest identity semantics

- [x] update: [processors/oldacl/utils.go](../../../pkg/processors/oldacl/utils.go)
  - implement: remove WSID-bearing role expansion and Principal formatting

- [x] update: [sys/authnz/impl_enrichprincipaltoken.go](../../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
  - implement: clone original payload roles and append only SubjectRoles using requestWSID and RoleScopeWorkspace; preserve API-token payloads without additions
  - reuse: issuePrincipalTokenRR from refresh for the enrichment response and remove duplicate enrichPrincipalTokenRR

### Validation

- [x] verify: scoped regressions and contract migration
  - run: focused tests for iauthnz, iauthnzimpl, authnz, storages, state providers, processors, command/query v1/v2, oldacl, and N10n
  - run: `go test ./pkg/sys/it -run '^TestForeignAuthorization$' -count=1`
  - run: `go -C pkg/iextengine/wazero/_testdata test ./tests/wasm -run '^Test_CommandStorages$' -count=1`
  - run: `go test ./... -run '^$'` to compile all root-module packages and tests without executing unrelated suites
  - run: scoped golangci-lint and `git diff --check`
  - review: original PrincipalPayload immutability, Subject-only WorkspaceDevice capture, unchanged initial grant scopes, exact added grants, and request isolation
  - previous validation: scoped unit tests, TestForeignAuthorization, nested WASM Test_CommandStorages, root-module compilation, scoped lint, formatting, relative file links, and whitespace checks passed before the latest test refactors

- [x] review: latest test refactors
  - verify: root assertion initialization, independent expected grants, shared mock expectations, removed vacuous checks, and retained production behavior coverage
  - run: focused package regressions and documentation link, changed-file coverage, and whitespace checks during synchronization
  - result: tests pass for coreutils, iauthnz, iauthnzimpl, itokens-payloads, itokensjwt, processors, command, query, query2, oldacl, N10n, stateprovide, authnz, and storages; teststate compiles with no test files
  - result: every changed source and specification file outside change folders is linked in the plan, relative file links resolve, and git diff --check passes

- [x] review: reuse merged AIR-5210 and AIR-5221 changes after rebasing onto main
  - verify: production retains the merged role-scope validation and issuance contracts; tests use the merged shared mocks without duplicating their implementations
  - verify: assertion setup follows AGENTS.md in all 28 changed root tests that use assertions; local doubles remain only where dependency cycles prevent sharing
  - result: focused package tests, full TestForeignAuthorization, nested WASM Test_CommandStorages, and lint for changes since main pass

- [x] review: preserve initial grants and capture only Subject roles in the request Workspace
  - result: affected authentication, token, processor, state, and storage package tests pass; TestForeignAuthorization passes with explicit HTTP status checks and no retries for the access checks
  - result: temporary Go overlays confirm that the VIT test detects owned-by additions, ignored regular-token exact grants, and exact grants incorrectly applied to child Workspaces
  - result: production-path tests detect temporary regressions that capture every effective role or drop a Subject assignment already effective from another source
  - result: all root-module packages and tests compile with `go test ./... -run '^$'`
  - result: scoped lint reports no issues; formatting, root assertion setup, relative links, and whitespace checks pass
  - result: Gherkin structural checks pass for eight scenarios and thirteen cases; behavior coverage uses the mapped Go tests without executable Gherkin bindings

- [x] review: consolidated tests and unused interfaces
  - preserve: TestBasicUsage; move canonical and Alias contribution checks into TestAuthenticationPayload and remove overlapping authentication cases
  - verify: repeat enrichment while the Subject still grants the role; retain one API-token preservation check and separate refresh response and access checks
  - remove: obsolete lookup-count checks, unused GetPrincipals methods and factory type, and the duplicate token response type
  - result: affected coreutils, authentication, processor, state provider, authnz, and storage package tests pass; teststate compiles with no test files; TestRefreshPrincipalToken and TestForeignAuthorization pass in VIT
  - result: changed-file coverage, relative plan links, relevant Gherkin structure, and whitespace checks pass; direct workpiece accessor, release, and failure-cleanup tests are not claimed as coverage
