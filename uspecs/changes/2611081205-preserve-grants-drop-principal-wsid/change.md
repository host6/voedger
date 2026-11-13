---
change_id: 2611081205-preserve-grants-drop-principal-wsid
type: fix
issue_url: https://untill.atlassian.net/browse/AIR-5213
domains: [prod]
scope: [auth, apps]
breaking: true
---

# Change request: Preserve enrichment grants and remove Principal.WSID

Refs:

- [AIR-5213: Remove Principal.WSID and separate effective principals from token grants](./issue-AIR-5213.md)

## Why

`Principal.WSID` conflates role applicability with Subject identity: it denotes the request Workspace for a Role Principal and the ProfileWorkspace for a User or Device Principal. Authentication projects token grants into effective Principals without retaining each role's original Scope, WSID, or source.

Enrichment reconstructs grants from those effective Principals. A role supplied only by an existing grant can therefore become another grant bound to the request Workspace, expanding access to another ownership level. Retaining the original grant does not prevent this additional access.

Problematic example:

- Workspace 100 owns Workspace 200, and Workspace 200 owns Workspace 300
- A regular token contains `{Scope: RoleScopeWorkspacesOwnedBy, WSID: 100, QName: sys.WorkspaceOwner}`
- That grant authorizes Workspace 200 but does not authorize Workspace 300
- No Subject, ownership, or GlobalRole independently grants WorkspaceOwner to the User in Workspace 200
- Authentication in Workspace 200 produces an effective WorkspaceOwner Principal from the carried grant

| Request                                                        | Workspace | Actual result                                                                                                           |
| -------------------------------------------------------------- | --------- | ----------------------------------------------------------------------------------------------------------------------- |
| An operation requiring WorkspaceOwner using the original token | 300       | 403 Forbidden                                                                                                           |
| q.sys.EnrichPrincipalToken using the original token            | 200       | 200 OK; EnrichedToken additionally contains `{Scope: RoleScopeWorkspacesOwnedBy, WSID: 200, QName: sys.WorkspaceOwner}` |
| The same operation using EnrichedToken                         | 300       | 200 OK                                                                                                                  |

The original grant remains bound to Workspace 100, while the added grant opens access to Workspace 300. The effective role alone does not distinguish a carried grant from an independent role assignment in Workspace 200.

## What

- Preserve every existing grant tuple and identity claim, including unmatched grants, canonical Login, Alias, SubjectKind, ProfileWSID, GlobalRoles, and IsAPIToken
- Preserve parent-to-child delegation for independently supplied roles, including Subject assignments and ownership-derived WorkspaceOwner without an owner Subject
- Add regular-token grants with `{Scope: RoleScopeWorkspacesOwnedBy, WSID: requestWSID, QName: role}` only when an eligible source independently supplies the role in the request Workspace
- Keep roles supplied only by carried grants effective for authorization without using them to create grants for another ownership level
- Bind new grants to the request Workspace; retain all original scopes and Workspace bindings unchanged
- Preserve API-token payloads without additions and retain their exact-Workspace authorization restrictions
- Deduplicate additions by the complete `{Scope, WSID, QName}` tuple without changing or removing original entries
- Separate request-local effective Principals, immutable token identity and grants, and roles eligible for enrichment; remove Principal.WSID
- Preserve endpoint URLs, bearer authentication, Login argument, EnrichedToken response field, existing endpoint authorization, and issuance lifetime
- Preserve complete payload and scope information through refresh
- Preserve exact access to a foreign Workspace through enrichment and refresh without granting access to its parent or children; a Subject's ProfileWSID identifies an identity and does not define a grant target

## How

Approaches considered:

- Reconstruct new grants from all effective Principals: loses role origin and permits the ownership-level expansion in Why
- Exclude every effective role whose QName occurs in a carried grant: suppresses legitimate enrichment when a Subject or another independent source grants the same role
- Collect independently supplied enrichment roles during authentication alongside the original payload and effective Principals: selected because it preserves required delegation, distinguishes overlapping sources, and prevents carried-grant-only expansion without a full provenance model

Decisions:

- Return one AuthnResult containing the original validated PrincipalPayload, effective Principals, and distinct EnrichmentRoles for the request
- Keep effective Principals free of Workspace identifiers; use request context for applicability and PrincipalPayload for Subject identity
- Record enrichment eligibility when a role source is evaluated, independently of whether effective-Principal deduplication already recorded the same role
- Eligible sources are Subject assignments resolved through canonical Login and Alias, GlobalRoles, profile ownership, Workspace ownership, Device eligibility, and the existing derived System role
- Matching carried grants contribute effective Principals only; they never contribute EnrichmentRoles by themselves, regardless of whether their scope is Workspace or WorkspacesOwnedBy
- Automatically supplied authentication markers such as AuthenticatedUser and Anonymous do not contribute EnrichmentRoles; API-token and synthetic Guest results provide no enrichment roles
- Keep role composition order and System short-circuits; collect eligible roles at their existing source stages, including before an early return where applicable
- Keep one set of distinct role names for enrichment rather than tagging every Principal or retaining a separate source record per grant
- Let authentication identify eligible role names and let enrichment construct the new owned-by tuples using requestWSID; authentication never changes payload grants
- Clone the original Roles slice before appending additions, preserving the authentication result and its backing storage
- Keep an explicit API-token guard that adds no grants, including grants for roles that are effective during API authentication
- Read the authentication result from the request workpiece without reauthentication, repeated Subject reads, or repeated Device eligibility checks
- Retain existing authorization inheritance and legacy ACL expansion; they do not independently materialize additional enrichment roles
- Repeated enrichment in one Workspace with unchanged independent role sources is idempotent; existing duplicate payload entries are preserved
- Re-enrichment in a child adds a role grant for its children only if an eligible source independently supplies that role there; authorization to call the endpoint alone does not establish eligibility for each new grant
- Keep existing scope-aware validation, legacy scope normalization, exact-grant matching, and scope-preserving serialization; match owned-by grants against the immediate owner's WSID without recursive ownership matching
- Preserve failure-result clearing, request isolation, canonical identity in RequestSubject, and identity availability when API-token or System composition omits a User or Device Principal
- Mark exported Go interface changes and the restriction on carried-grant-only redelegation as breaking; preserve the endpoint protocol and independently sourced parent-to-child delegation
- Express requirements, specifications, code, comments, and tests using platform concepts

Acceptance criteria:

- A freshly signed-in owner of Workspace 100 with ProfileWSID 42, no carried grants, and no matching Subject enriches at 100 and gains access to its immediate child 200
- A Subject-assigned role in Workspace 100 can be delegated to its children without separate Subject assignments in those children
- A token with only an owned-by WorkspaceOwner grant bound to 100 can use Workspace 200 but gains no WorkspaceOwner grant bound to 200 when enriched there, leaving access to child 300 forbidden
- If a Subject independently grants the same WorkspaceOwner role in 200, enrichment there adds an owned-by grant bound to 200 and access to 300 succeeds
- The same distinction applies to roles carried with exact-Workspace scope; their original tuples remain unchanged
- A role shared by a carried grant and an eligible source remains eligible, and each new tuple is added at most once
- A Device role independently supplied by Subjects, GlobalRoles, or eligibility uses requestWSID for its new grant even when ProfileWSID differs
- Original mixed-scope and unmatched grants and all identity fields remain unchanged, and refresh preserves the enriched payload and its access boundaries
- With ProfileWorkspace 100 owning 200 and ProfileWorkspace 300 owning 400, which owns 500, an original exact WorkspaceOwner grant for 400 survives enrichment in 100 and refresh: operations in 400 succeed while the same operations in 300 and 500 remain forbidden
- A matching Subject stored in 400 for the User with ProfileWSID 100 permits operations in 400 without a carried grant, while 300 and 500 remain forbidden; enrichment in 100 does not read that Subject or materialize its role into the token
- A Subject stored in 100 does not define an exact grant for the Workspace named by its ProfileWSID; absent separate authority for 400, the stored record alone cannot make operations in 400 succeed
- API-token enrichment adds no grants and creates no child access
- Missing bearer tokens fail without publishing a token, and normal endpoint authorization remains required

Assumptions:

- None

Out of scope:

- New endpoint parameters, a separate delegation API, or per-role grant revocation
- Changes to role inheritance, legacy authorization policy, or token expiration policy
- A general role-provenance model beyond the enrichment eligibility snapshot

References:

- [Authentication boundary](../../../pkg/iauthnz/authn-interface.go)
- [Principal and identity types](../../../pkg/iauthnz/authn-types.go)
- [Role source composition](../../../pkg/iauthnzimpl/impl.go)
- [Token enrichment endpoint](../../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
- [Principal payload and grant scopes](../../../pkg/itokens-payloads/types.go)
- [Scope validation and legacy normalization](../../../pkg/itokens-payloads/impl_principalpayload.go)
- [Child Workspace authorization tests](../../../pkg/sys/it/impl_childworkspace_test.go)
- [Endpoint declaration and authorization](../../../pkg/sys/sys.vsql)
- [Authorization role expansion](../../../pkg/processors/oldacl/utils.go)

## Domain design

- [x] update: [prod/domain.md](../../specs/prod/domain.md)
  - distinguish: request-local Principal authorization from Subject identity and token-carried RoleGrant values

- [x] update: [prod/auth/context.md](../../specs/prod/auth/context.md)
  - define: WSID-free effective Principals and identity obtained from the immutable original token payload
  - define: enrichment eligibility from independent role sources, original grant preservation, owned-by additions bound to the request Workspace, unchanged API-token payloads, and full tuple equality
  - distinguish: effective authorization from eligibility to create another grant, including roles shared by carried grants and independent assignments
  - specify: immediate-owner applicability, idempotent repeated enrichment, and prevention of carried-grant-only propagation to another ownership level
  - distinguish: Subject identity and the Workspace containing its assignment from a RoleGrant target; Subject.ProfileWSID is never interpreted as a target binding

## Functional design

- [x] create: [auth/token-enrichment.feature](../../specs/prod/auth/token-enrichment.feature)
  - Feature specification implementing every case in [Required enrichment scenarios](#required-enrichment-scenarios), including exact foreign-Workspace access, Subject placement, parent-to-child delegation, source-dependent further delegation, claims and grant preservation, API-token restrictions, repetition, refresh, and authentication failures

- [x] update: [auth/authn.feature](../../specs/prod/auth/authn.feature)
  - specify: refresh with no grants or mixed owned-by and exact grants preserves the complete PrincipalPayload, Application, issued lifetime, response ProfileWSID, expiration, and Content-Type

### Required enrichment scenarios

Cover every row with a Scenario or meaningful Examples rows. Combine groups that share a coherent history and behavior, retaining their coverage tags; do not require a separate Scenario for each group. Start each behavior group with basic usage and give every Scenario exactly one When and one Then. Multi-request behaviors use ordered request and response tables with automatic retries disabled for access checks. Denial cases exclude other applicable ownership, Subjects, GlobalRoles, System authority, and carried grants.

| ID  | Required behavior                                                         | Required cases and observable outcomes                                                                                                                                                                                                                                                                                                               |
| --- | -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| E1  | A parent owner delegates access to immediate children                      | ProfileWorkspace 42 owns 100, which owns 200; without carried grants or matching Subjects, enrichment at 100 adds the owned-by WorkspaceOwner grant bound to 100 and changes a protected operation in 200 from 403 Forbidden to 200 OK                                                                                                               |
| E2  | A Subject-assigned role grants access to children of its Workspace         | A foreign User has a Subject assignment in 100 through canonical Login or active Alias; enrichment at 100 permits the corresponding child operation without a Subject in the child                                                                                                                                                                   |
| E3  | Enrichment captures roles from independent non-Subject sources             | Meaningful Examples cover GlobalRoles, profile ownership, and the derived System role; assert the precise added grant tuples, unchanged identity, and absence of grants for automatically supplied authentication markers                                                                                                                            |
| E4  | An independently supplied Device role uses the request Workspace           | Examples cover a Subject, GlobalRoles, and Device eligibility, including overlap with a carried role; with ProfileWSID 100 and request Workspace 200, add one eligible owned-by grant bound to 200 without rebinding carried grants                                                                                                                  |
| E5  | Enrichment creates a further grant only from an independent assignment     | For both exact and owned-by carried WorkspaceOwner grants authorizing 200, enrichment at 200 alone leaves 300 forbidden; an independent Subject assignment in 200 permits the new grant bound to 200 and changes access to 300 to 200 OK; refresh preserves the applicable boundary                                                                  |
| E6  | Enrichment and refresh preserve an exact grant to a foreign Workspace      | ProfileWorkspace 100 owns 200, and ProfileWorkspace 300 owns 400, which owns 500; the token for ProfileWSID 100 already carries an exact WorkspaceOwner grant for 400; after enrichment in 100 and refresh, 400 returns 200 OK while 300 and 500 return 403 Forbidden; enrichment in 400 without an independent source must also leave 500 forbidden |
| E7  | A Subject identity reference does not select a grant target                | A Subject stored in 100 contributes roles only through the matching Login or Alias in 100; its ProfileWSID never creates a target grant for 400; with no original grant or separate membership authorizing 400, enrichment in 100 leaves operations in 400 forbidden                                                                                 |
| E8  | Membership in a foreign Workspace authorizes only that Workspace           | For the E6 hierarchy, a Subject stored in 400 grants WorkspaceOwner to the User whose ProfileWSID is 100, and the token carries no grant for 400; after enrichment in 100, membership lookup in 400 permits access there while 300 and 500 remain forbidden, without a grant for 400 appearing in the returned token                                 |
| E9  | Enrichment preserves identity and original grant tuples                    | User and Device Examples preserve Login, Alias, SubjectKind, ProfileWSID, GlobalRoles, IsAPIToken, and every original mixed-scope and unmatched tuple; legacy missing scopes retain regular-token owned-by semantics; assert the exact allowed additions, Application binding, and DefaultPrincipalTokenExpiration                                   |
| E10 | Repeated enrichment preserves the payload without duplicate additions      | Enrich twice in one Workspace with unchanged independent sources; the returned payloads match, additions occur once, original duplicate entries remain unchanged, and grants sharing a QName remain distinct when Scope or WSID differs                                                                                                              |
| E11 | An issued delegation remains valid after its Subject assignment is removed | After enriching a Subject-assigned role at 100 and removing that Subject, the carried grant still permits the child operation in 200; it does not authorize an unrelated branch or grandchild 300 owned by 200                                                                                                                                       |
| E12 | API-token enrichment preserves its payload and exact access boundaries     | An API token authorized to enrich in its exact Workspace retains its complete payload despite effective roles and independent Subject assignments; legacy missing scopes retain API-token exact-Workspace semantics; no new grants or child access appear                                                                                            |
| E13 | Enrichment refuses to issue a token without a bearer token                 | Executor invocation with synthetic Guest identity and no bearer returns ErrInvalidToken and publishes no token; keep this error assertion separate from HTTP authorization outcomes                                                                                                                                                                  |
| E14 | Enrichment rejects a caller without authority in the request Workspace     | A valid enrichment request with a valid token but no applicable role authorizing the endpoint receives 403 Forbidden and no EnrichedToken                                                                                                                                                                                                            |
| E15 | Enrichment rejects an invalid bearer token                                 | For valid request bodies, meaningful Examples cover invalid signature, expiration, wrong Application audience, and invalid explicit role scopes; each receives 401 Unauthorized and no EnrichedToken                                                                                                                                                 |

Reuse the refresh response and payload Scenario Outline in [Authentication](../../specs/prod/auth/authn.feature) rather than duplicating it. E5 and E6 cover access boundaries after refresh; they must verify authorization as well as decoded claims. Preserve Subject identity semantics in E7 and E8 instead of introducing a target field or treating ProfileWSID as a RoleGrant.

Map every Scenario and Examples row to named production-path Go tests with verbatim step and Examples comments. Source-read counts, Roles slice backing-storage isolation, authentication-result reset, and other internal invariants remain Go test responsibilities; Gherkin outcomes use returned token claims, response fields, errors, and authorization results.

## Technical design

- [ ] update: [prod/auth/arch.md](../../specs/prod/auth/arch.md) and [prod/auth/arch-authz.md](../../specs/prod/auth/arch-authz.md)
  - define: AuthnResult with original PrincipalPayload, effective Principals, and EnrichmentRoles, including Guest, API-token, System, and error paths
  - document: eligible role collection at source stages independently of effective-role deduplication; carried grants contribute only to authorization
  - document: canonical Login and Alias lookups, ownership and Device-derived roles, exact-grant matching without a descriptor, existing System short-circuits, and atomic failure results

- [ ] update: [prod/auth/arch-tokens.md](../../specs/prod/auth/arch-tokens.md)
  - document: immutable payload copying and new owned-by tuples from EnrichmentRoles using requestWSID, with complete tuple deduplication and an explicit API-token guard
  - specify: re-enrichment preserves carried bindings and cannot create a further grant from a carried role alone; independent assignments remain eligible even when the role is already effective
  - document: existing endpoint authorization, response shape, DefaultPrincipalTokenExpiration, no additional source reads, scope-aware validation, legacy scope normalization, and scope-preserving refresh and serialization

- [x] update: [prod/auth/authn--td.md](../../specs/prod/auth/authn--td.md)
  - document: refresh preserves the complete PrincipalPayload, including grant scopes and bindings, Application, and issued lifetime

- [ ] update: [prod/apps/arch-processing.md](../../specs/prod/apps/arch-processing.md)
  - document: authentication-result propagation and reset across command, query v1/v2, and subscription processing
  - define: PrincipalPayload for identity, Principals for authorization, and EnrichmentRoles for endpoint grant construction
  - document: RequestSubject identity projection, System and Guest checks, diagnostics, and verification ownership

## Construction

### Tests

- [x] update: [pkg/iauthnz/utils_test.go](../../../pkg/iauthnz/utils_test.go)
  - verify: request-local System role checks without Principal.WSID

- [ ] update: [pkg/iauthnzimpl/impl_test.go](../../../pkg/iauthnzimpl/impl_test.go)
  - verify: original payload preservation, User/Device/Guest/API/System identity, explicit and legacy scopes, role deduplication, and empty results on failure
  - verify: independent expected Principals and EnrichmentRoles for canonical Login, Alias, GlobalRoles, profile ownership, Workspace ownership, Device eligibility, and System paths
  - verify: carried exact and owned-by roles remain effective without becoming enrichment candidates; roles shared with independent sources remain candidates
  - verify: authentication markers and synthetic Guest and API-token results supply no enrichment candidates
  - reuse: shared MockAppStructs, MockRecords, and TestObject descriptors; retain explicit source reads and Device eligibility expectations

- [x] remove: `pkg/sys/storages/impl_subject_storage_test.go`
  - consolidate: RequestSubject identity projection, storage-key equality, and callback freshness checks into state-provider tests

- [x] update: [state/stateprovide/impl_host_state_test.go](../../../pkg/state/stateprovide/impl_host_state_test.go)
  - verify: PrincipalPayload-based identity with request Workspace different from ProfileWSID, Token retrieval, key equality, and fresh callback values

- [x] update: [pkg/processors/utils_test.go](../../../pkg/processors/utils_test.go)
  - verify: anonymous-only result replacement and synthetic Guest identity

- [x] update: [processors/query/impl_test.go](../../../pkg/processors/query/impl_test.go)
  - verify: a real query extension reads Login, SubjectKind, ProfileWSID, and Token through RequestSubject when no User or Device Principal is emitted

- [x] update: [processors/n10n/impl_test.go](../../../pkg/processors/n10n/impl_test.go)
  - verify: per-Workspace subscription authorization and unauthorized/forbidden response mapping using shared descriptor mocks

- [x] create: [processors/oldacl/impl_test.go](../../../pkg/processors/oldacl/impl_test.go)
  - Test reserved Guest identity matching against all identity fields
  - Verify authorization role ancestry and explicit legacy role expansion without changing the input
  - Verify Verbose denial diagnostics and suppression below Verbose severity

- [ ] create: [sys/authnz/impl_enrichprincipaltoken_test.go](../../../pkg/sys/authnz/impl_enrichprincipaltoken_test.go)
  - Exercise production authentication and enrichment with real JWTs and shared state, app, records, key, and value mocks
  - Verify independent expected owned-by tuples from eligible sources, including ownership-derived WorkspaceOwner without a Subject and canonical and Alias Subject roles
  - Verify carried-grant-only roles produce no additions while overlapping independent assignments remain eligible
  - Verify mixed-scope and unmatched original grants, distinct tuples sharing a QName, unchanged identity, Application, and endpoint lifetime
  - Verify original workpiece payload and spare backing capacity remain unchanged, and reauthentication followed by repeated enrichment creates no duplicate tuples
  - Verify request-bound Device grants from each eligible source and no repeated Subject reads or eligibility checks
  - Verify API-token payload equality despite effective roles and missing-bearer rejection without publishing a token
  - Keep the workpiece double local only to avoid an import cycle; reuse shared mocks for general dependency boundaries
  - Map E3, E4, E9, E10, E12, and E13 to named production-path cases; coordinate E1, E2, and E5 with authentication-source and VIT assertions rather than duplicating complete fixtures

- [x] update: [sys/it/impl_authn_feature_test.go](../../../pkg/sys/it/impl_authn_feature_test.go)
  - verify: complete refresh payload, Application, lifetime, ProfileWSID, expiration, Content-Type, and new issuance through the real endpoint with no grants and mixed-scope grants

- [ ] update: [sys/it/impl_childworkspace_test.go](../../../pkg/sys/it/impl_childworkspace_test.go)
  - verify: successful parent enrichment and child access separately for an ownership-derived owner without a Subject and a foreign User with a Subject-assigned role
  - verify: a fresh regular token has no carried grants, child access is initially forbidden, and the enriched token permits the child operation without child membership
  - verify: an owned-by grant bound to 100 permits Workspace 200, but enrichment at 200 without an independent role source leaves Workspace 300 forbidden
  - verify: adding an independent Subject assignment in 200 enables its new owned-by grant and access to 300 while preserving the grant bound to 100
  - verify: the carried-grant-only restriction also applies to exact grants; existing exact-grant isolation and immediate-owner matching remain valid
  - verify: complete expected tuples, independently expected identity, unmatched-grant preservation, same-Workspace idempotency, and unchanged access boundaries after refresh
  - verify: API-token enrichment preserves exact scope and creates no new child access
  - verify: E6 and E8 through one foreign-access outline covering exact grants and live membership; check the 400/300/500 boundary after enrichment, unchanged claims through refresh, and source-dependent child access after re-enrichment in 400
  - verify: E7 does not infer foreign authority from Subject.ProfileWSID, and E8 obtains authority from a matching Subject stored in 400 without adding a token grant for 400
  - verify: E11 preserves issued child access after source Subject removal; do not mistake the carried grant for authority in its source Workspace
  - map: E1, E2, E5, E6, E7, E8, E11, E12, E14, and E15 to named endpoint-level cases, reusing existing scope and authentication checks where their setup and assertions prove the complete Scenario
  - use: shared VIT setup, explicit expected HTTP statuses, no automatic retries for access checks, and feature-to-test traceability with verbatim steps and Examples rows

- [x] update: [tests/wasm/main_test.go](../../../pkg/iextengine/wazero/_testdata/tests/wasm/main_test.go)
  - verify: extension-visible SubjectKind, Login, ProfileWSID, and Token through PrincipalPayload-based test-state fixtures

### Authentication and request state

- [x] update: [pkg/coreutils/mock.go](../../../pkg/coreutils/mock.go)
  - extend: shared MockAppStructs with ClusterAppID and nil-safe AppTokens expectations, retaining existing interface compliance checks
  - reuse: shared records and other interface mocks instead of duplicating general dependency boundaries

- [ ] update: [pkg/iauthnz/authn-types.go](../../../pkg/iauthnz/authn-types.go)
  - remove: Principal.WSID
  - define: AuthnResult with PrincipalPayload, Principals, and distinct EnrichmentRoles

- [x] update: [pkg/iauthnz/authn-interface.go](../../../pkg/iauthnz/authn-interface.go)
  - return: AuthnResult and error from Authenticate
  - remove: the unused NewDefaultAuthenticatorType declaration

- [x] update: [pkg/iauthnz/utils.go](../../../pkg/iauthnz/utils.go)
  - implement: request-local System checks and shared synthetic Guest identity construction

- [ ] update: [pkg/iauthnzimpl/impl.go](../../../pkg/iauthnzimpl/impl.go)
  - implement: original payload retention, WSID-free effective-role deduplication, source-stage EnrichmentRoles collection, and complete result clearing on failure
  - collect: eligible roles from canonical and Alias Subjects, GlobalRoles, profile and Workspace ownership, Device eligibility, and the existing derived System role
  - separate: eligible-role insertion from effective-role deduplication so overlapping sources cannot suppress legitimate candidates
  - apply: carried exact and owned-by grants to effective Principals without adding enrichment candidates; provide no candidates for API tokens, synthetic Guest, or authentication markers
  - preserve: source evaluation order and System short-circuits; evaluate Device eligibility at its source stage independently of already-effective role values
  - match: exact grants by requestWSID independently of a descriptor and owned-by grants by immediate OwnerWSID

- [x] update: [pkg/state/types.go](../../../pkg/state/types.go)
  - replace: Principal callbacks with PrincipalPayload callbacks for command and query identity

- [x] update: [state/stateprovide/impl_command_processor_state.go](../../../pkg/state/stateprovide/impl_command_processor_state.go) and [state/stateprovide/impl_query_processor_state.go](../../../pkg/state/stateprovide/impl_query_processor_state.go)
  - wire: PrincipalPayload providers into RequestSubject

- [x] update: [sys/storages/impl_subject_storage.go](../../../pkg/sys/storages/impl_subject_storage.go)
  - project: identity from PrincipalPayload while retaining storage fields and the Token callback

- [x] update: [state/teststate/impl.go](../../../pkg/state/teststate/impl.go) and [state/teststate/interface.go](../../../pkg/state/teststate/interface.go)
  - accept: explicit PrincipalPayload fixtures in PutRequestSubject and wire them into state providers

### Request processors and authorization

- [x] update: [pkg/processors/types.go](../../../pkg/processors/types.go)
  - expose: the shared authentication result through GetAuthnResult in place of GetPrincipals

- [x] update: [pkg/processors/utils.go](../../../pkg/processors/utils.go)
  - initialize: anonymous-only authentication results with synthetic Guest identity and no enrichment authority

- [x] update: [processors/command/impl.go](../../../pkg/processors/command/impl.go) and [processors/command/types.go](../../../pkg/processors/command/types.go)
  - propagate: authentication results, reset request state, project PrincipalPayload identity, and migrate System checks and legacy authorization
  - preserve: existing Verbose fallback-success diagnostics

- [x] update: [processors/query/impl.go](../../../pkg/processors/query/impl.go)
  - propagate: authentication results through query v1 authorization and extension execution; expose GetAuthnResult and reset the result on release

- [x] update: [processors/query2/impl.go](../../../pkg/processors/query2/impl.go) and [processors/query2/util.go](../../../pkg/processors/query2/util.go)
  - propagate: authentication results through query v2 authorization and state construction, expose GetAuthnResult, and reset results on release

- [x] update: [query2/impl_auth_refresh_handler.go](../../../pkg/processors/query2/impl_auth_refresh_handler.go)
  - populate: refresh response ProfileWSID from PrincipalPayload

- [x] update: [query2/impl_cdocs_handler.go](../../../pkg/processors/query2/impl_cdocs_handler.go), [query2/impl_docs_handler.go](../../../pkg/processors/query2/impl_docs_handler.go), and [query2/impl_view_handler.go](../../../pkg/processors/query2/impl_view_handler.go)
  - pass: effective Principals and PrincipalPayload to legacy result authorization while retaining existing diagnostics

- [x] update: [processors/n10n/impl_subscribeandwatch.go](../../../pkg/processors/n10n/impl_subscribeandwatch.go)
  - use: each subscription Workspace's authentication result independently

- [x] update: [processors/oldacl/acl.go](../../../pkg/processors/oldacl/acl.go), [processors/oldacl/impl.go](../../../pkg/processors/oldacl/impl.go), and [processors/oldacl/utils.go](../../../pkg/processors/oldacl/utils.go)
  - match: Guest access using PrincipalPayload identity, and role access using request-local Principals without WSID
  - preserve: authorization ancestry and legacy role expansion, input immutability, and Verbose diagnostics
  - remove: WSID from Principal formatting

### Token enrichment

- [ ] update: [sys/authnz/impl_enrichprincipaltoken.go](../../../pkg/sys/authnz/impl_enrichprincipaltoken.go)
  - require: a nonempty bearer token and obtain the existing authentication result from IProcessorWorkpiece
  - copy: PrincipalPayload and clone its Roles slice
  - append: only regular-token EnrichmentRoles as `{Scope: RoleScopeWorkspacesOwnedBy, WSID: args.WSID, QName: role}` when the full tuple is absent
  - preserve: all original tuples and identity fields, add no API-token grants, and do not reconstruct candidates from effective Principals
  - issue: with the existing Application binding and DefaultPrincipalTokenExpiration, without further source reads or validation
  - reuse: issuePrincipalTokenRR from refresh for EnrichedToken and remove the duplicate enrichPrincipalTokenRR type

### Validation

- [ ] verify: enrichment sources and ownership boundaries
  - run: `go test ./pkg/iauthnz ./pkg/iauthnzimpl ./pkg/sys/authnz -count=1`
  - run: `go test ./pkg/sys/it -run '^TestForeignAuthorization$|^TestRoleScopedToOneForeignWorkspace$|^TestRefreshPrincipalToken$' -count=1`
  - verify: direct parent ownership and Subject roles enable child access, carried roles alone cannot enable another ownership level, and independent overlapping sources remain eligible
  - verify: original payload immutability, complete tuple deduplication, API-token non-expansion, and scope-preserving refresh

- [ ] verify: authentication-result integration
  - run: focused tests for coreutils, itokens-payloads, itokensjwt, processors, command, query, query2, oldacl, N10n, stateprovide, and storages without repeating packages already validated unless changes or failures require it
  - run: `go -C pkg/iextengine/wazero/_testdata test ./tests/wasm -run '^Test_CommandStorages$' -count=1`
  - run: `go test ./... -run '^$'` to compile root-module packages and tests without executing unrelated suites
  - verify: canonical identity, API and System identity, Guest matching, request isolation, explicit scope semantics, legacy normalization, and existing endpoint authorization

- [ ] review: platform design and implementation
  - compare: the complete change against main and the requirements in What, including the grandchild failure in Why and required parent-to-child delegation
  - verify: one authoritative enrichment eligibility snapshot, no duplicated source lookups or candidate abstractions, and no dependence on Principal.WSID
  - verify: specifications share canonical grant and eligibility rules and contain no consumer-specific names or dependencies
  - verify: every modified root test initializes require once as its first statement; shared mocks and independent expectations exercise production behavior
  - verify: feature Scenario names, verbatim step and Examples mappings, exactly one When and Then per Scenario, and precise observable outcomes
  - verify: every E1-E15 case and every acceptance criterion has a written Scenario or Examples row and mapped production assertions; token-claim checks alone do not prove the required 400/300/500 or parent/child/grandchild access boundaries
  - run: formatting, scoped lint, relative-link and changed-file coverage checks, and `git diff --check`
  - report: only the unit, VIT, compilation, and source-review checks actually performed
