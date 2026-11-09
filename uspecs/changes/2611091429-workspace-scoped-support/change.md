---
change_id: 2611091429-workspace-scoped-support
type: test
issue_url: https://untill.atlassian.net/browse/AIR-5083
domains: [prod]
scope: [auth, apps]
---

# Change request: Exact-Workspace role authorization coverage

Refs:

- [AIR-5083 issue snapshot](./issue-AIR-5083.md)

## Why

A token must be able to grant a role for an exact Workspace without granting that role in its parent, children, or unrelated Workspaces. The platform already implements exact-Workspace role scopes; VIT integration coverage must demonstrate that real request authorization enforces this boundary.

## What

- VIT integration tests prove that a token carrying an exact-Workspace role authorizes an operation granted to that role in the specified Workspace
- The same token cannot authorize that operation through this role in any other Workspace, including its parent, children, and unrelated Workspaces
- A dedicated VIT test verifies that a token for a foreign Workspace grants no access to the caller's own Workspace, the foreign Workspace owner's profile, the caller's profile, or another Workspace owned by the foreign owner
- Existing authorization behavior remains unchanged; completion requires integration-test evidence rather than new authentication logic unless the tests expose a defect

## How

Decisions:

- Reuse the existing API-token and RoleScopeWorkspace contracts introduced by AIR-5210 and AIR-5213; no new token type or application-specific role is needed
- Extend TestForeignAuthorization/API_token instead of adding a parallel VIT suite; it already proves success in the specified Workspace, an exact-Workspace grant in the token, and HTTP 403 in the parent Workspace
- Add the missing child and unrelated Workspace checks using the same signed token and protected query, with the same role and resource grants available in every tested Workspace
- Exercise real VIT request processing and ACL evaluation; do not configure an authenticator mock to return the expected authorization decision

Assumptions:

- None

Out of scope:

- Application-specific workflows, token exchange, and permission inventories
- New identity-attribution or WLog audit behavior
- New token-expiry behavior or changes to public and anonymous-only endpoints

References:

- [Existing VIT exact-Workspace authorization test](../../../pkg/sys/it/impl_childworkspace_test.go)
- [Existing VIT role and query grants](../../../pkg/vit/schemaTestApp1.vsql)
- [Exact-Workspace API-token issuance](../../../pkg/iauthnzimpl/utils.go)
- [Request-local role composition](../../../pkg/iauthnzimpl/impl.go)
- [Role scope semantics](../../specs/prod/auth/context.md#rolescope)

## Construction

- [x] update: [sys/it/impl_childworkspace_test.go](../../../pkg/sys/it/impl_childworkspace_test.go)
  - retain: TestForeignAuthorization/API_token success in the specified Workspace, explicit RoleScopeWorkspace grant verification, and denial in the parent Workspace
  - add: deny the same protected query with the same token in a child of the specified Workspace and in an unrelated Workspace
  - verify: every tested Workspace has the same role and resource grants, so HTTP 403 demonstrates token scope rather than missing schema or unrelated ACL differences
  - follow: root assertion initialization and brief comments explaining the authorization boundary each check protects

- [x] verify: VIT exact-Workspace authorization coverage
  - run: `go test ./pkg/sys/it -run '^TestForeignAuthorization$/^API_token$' -count=1`
  - review: the test must fail if exact-Workspace matching is removed, treated as global, or replaced by ownership-based matching
  - record: the existing and added integration cases that establish the goal, and any production defect exposed by them
  - result: focused API-token coverage and the full TestForeignAuthorization pass; each other Workspace accepts its own control token and rejects the original token
  - result: temporary Go overlays confirm the test fails when role grants are omitted, Workspace matching is ignored, or exact matching is replaced by ownership matching; no production defect was found

- [x] update: [sys/it/impl_childworkspace_test.go](../../../pkg/sys/it/impl_childworkspace_test.go)
  - add: TestExactWorkspaceRoleDoesNotAuthorizeOwnedOrRelatedWorkspaces as a separate VIT test with distinct caller and foreign Workspace owner identities
  - verify: one signed token works in the foreign target and is denied in the caller-owned Workspace, foreign owner's profile, caller's profile, and another Workspace of the foreign owner
  - verify: an exact-Workspace control token succeeds in each negative case, using the common WorkspaceOwner role and ChildWorkspace collection to exclude schema differences
  - result: the dedicated test passes through real application token signing and request authorization without mocks or schema changes
