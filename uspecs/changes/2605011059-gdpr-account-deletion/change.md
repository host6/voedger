---
registered_at: 2026-05-01T10:59:07Z
change_id: 2605011059-gdpr-account-deletion
baseline: 4f210eb8ab63962037c3dc2e58456093f27056a2
issue_url: https://untill.atlassian.net/browse/AIR-3647
---

# Change request: GDPR account deletion and data removal

## Why

A user has requested deletion of their account and associated personal data, and there is currently no defined process to handle such requests in compliance with GDPR. See [issue.md](issue.md) for details.

## What

Account deletion mechanism that preserves data integrity while making personal data inaccessible:

- Deactivate user profile, `cdoc.registry.Login` and all child workspaces instead of physically deleting or overwriting personal data
- Deactivate invitation-related docs (`cdoc.sys.Invite`, `cdoc.sys.Subject`, etc.) so the same login can be re-invited after re-sign-up

Behavior guarantees after deletion, covered by integration tests:

- Sign-up under the same login produces a new empty profile
- A new login under the same email/identifier can be invited into workspaces where the deleted login was previously invited
- Sign-in under a deleted account reports the login as non-existent (not deactivated)

