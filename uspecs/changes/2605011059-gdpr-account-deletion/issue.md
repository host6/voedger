# AIR-3647: GDPR — Implement account deletion and data removal

- **Key**: [AIR-3647](https://untill.atlassian.net/browse/AIR-3647)
- **Type**: Task
- **Status**: In Progress
- **Assignee**: d.gribanov@dev.untill.com
- **Reporter contact**: s.w.korf@gmail.com

## Motivation

A user has requested deletion of their account and associated personal data (email and phone number) — request received around middle of April, 20.04.2026.

Currently, there is no clearly defined or user-friendly process for:

- Deleting an account
- Removing personal data
- Ensuring compliance with legal requirements (e.g., GDPR)

## Acceptance criteria

All personal data is removed:

- Email
- Phone number
- Associated personal identifiers

All customer data should be deleted before 18.05.2026.

## Functional design

### User story

As a user, I want my account and personal data to be deleted upon request, so that my privacy rights are respected and my data is no longer stored in the system.

## Proposed technical design

### Common approach

- Do not actually delete anything: neither delete, nor overwrite personal data with empty values
- Deactivate user profile, `cdoc.registry.Login` and all child workspaces
- Do not touch docs that actually contain the important data like phones, emails, names etc. Make it impossible to get them by deactivating workspaces
- Deactivate all docs that describe invitation of the account to other workspaces: walk through `cdoc.sys.Invite`, `cdoc.sys.Subject` etc. This is needed to make it possible to re-invite after re-sign-up under the same login

### Critical requirements

The following must be possible after deletion and must be comprehensively covered by integration tests:

- Sign-up under the same login. After that it must be seen that it is a new empty just-created profile
- Invite the new login into a workspace where that old login was already invited
- Sign-in under a deleted account must return that the login does not exist, not that the login is deactivated
- Low-level `TestGenerateAdminTokenByLogin` db util must return an error saying that the account is deleted

## See also

- <https://github.com/voedger/voedger/issues/37>
- Google Chat discussion (referenced in original issue)
