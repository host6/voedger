# Reseller access, voedger: remove Principal.WSID and separate effective principals from enrichment grants

- URL: https://untill.atlassian.net/browse/AIR-5213
- ID: AIR-5213
- State: in-progress
- Author: Denis Gribanov
- Labels: none
- Assignees: Denis Gribanov
- Linked issues: Parent AIR-1808; blocks AIR-5083

## Why

* Effective role principals apply to the current request workspace, so repeating that workspace in `Principal.WSID` is unnecessary
* For User and Device principals, `Principal.WSID` identifies the subject’s profile workspace, mixing identity with role applicability
* Token grants now carry explicit scope, but authentication projects them into effective principals that lose the source grant’s WSID and scope
* Enrichment reconstructs grants from those effective principals, implicitly creating grants for another ownership level

For example:

* Workspace `100` owns workspace `200`, which owns workspace `300`
* A regular token contains `{Scope: WorkspacesOwnedBy, WSID: 100, QName: WorkspaceOwner}`
* Authentication in workspace `200` emits an effective `WorkspaceOwner` principal associated with workspace `200`
* Enrichment turns that principal into `{Scope: WorkspacesOwnedBy, WSID: 200, QName: WorkspaceOwner}`
* The token gains access to workspace `300`, while retaining its original grant

Explicit token scopes make both grants understandable, but the effective principal cannot distinguish retaining an existing grant from creating a delegated grant.

## What

* Define effective principals as authorization inputs evaluated in the current request context
* Remove `Principal.WSID`, retaining the target application and workspace in request context
* Expose subject identity separately, including subject kind, canonical login, and `ProfileWSID`
* Update authorization consumers, System checks, RequestSubject storage, legacy Guest matching, and logging to use the appropriate context or subject identity
* Collect enrichment candidates separately from effective principals, retaining source information and any original `{Scope, WSID, QName}` grant
* Define enrichment eligibility and grant construction for token-carried, Subject, GlobalRole, and derived roles
* Make parent-to-child delegation an explicit policy, including whether repeated enrichment may extend access through additional ownership levels
* Preserve existing grants and deduplicate resulting grants by `{Scope, WSID, QName}`
* Test subject identity preservation, unchanged authorization decisions for existing tokens, and enrichment behavior across repeated requests and ownership levels

## Policy decision

Choose whether enrichment preserves today’s parent-to-child propagation or restricts it. Document the chosen behavior before implementation; any restriction changes effective permissions and needs explicit acceptance criteria.
