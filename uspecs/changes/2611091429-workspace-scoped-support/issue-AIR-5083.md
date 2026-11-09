# Reseller access: workspace-scoped support principal

- URL: https://untill.atlassian.net/browse/AIR-5083
- ID: AIR-5083
- State: in-progress
- Author: Michael Saigachenko
- Labels: none
- Assignees: Denis Gribanov
- Linked issues: Parent AIR-1808; blocked by AIR-5210 and AIR-5213; blocks AIR-5086, AIR-5087, and AIR-5088

## Scope

Resolve the platform authorization issue for temporary reseller support access.

* Allow a temporary token to contain the `ResellerSupport` role for exactly one client workspace.
* Retain the reseller's canonical user identity so WLog attributes actions to the specialist.
* Ensure the role is never global.
* Enforce token expiry on every server request.

## Completion gate

* Automated tests prove correct WLog identity attribution.
* The role is effective only in the approved workspace.
* The same token is denied in every other workspace.
* Expired tokens cannot read data or execute commands.

## Dependencies

None. This subtask blocks token exchange, permission rollout, and final backend verification.

Technical design: https://github.com/untillpro/airs-design/blob/main/resellerportal/reselleraccess.md
