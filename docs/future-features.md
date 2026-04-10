# Future Features

## User Management API

Currently users can only be created via env vars on startup (`SEAFILE_ADMIN_EMAIL`/`SEAFILE_ADMIN_PASSWORD`) or direct DB insert. Need proper API endpoints:

- `POST /api/v1/users` - create user
- `GET /api/v1/users` - list users
- `DELETE /api/v1/users/{email}` - delete user
- `PUT /api/v1/users/{email}` - update user (password, active status)

### Prerequisites

- **Admin check helper**: query `is_staff` from `EmailUser` for the authenticated user
- **Admin middleware or per-handler guard**: gate user management endpoints behind `is_staff=1`
- Decide: should `is_staff` also grant all-repo visibility in `CheckPerm` and `ListReposHandler`?

## Repo Sharing API

Users can only access repos they own. No way to share repos through the API yet.

- `POST /api/v1/repos/{id}/shares` - share repo to user/group
- `GET /api/v1/repos/{id}/shares` - list shares
- `DELETE /api/v1/repos/{id}/shares/{email}` - revoke share

## Prevent Repo Creation for Non-Admin Users

Seafile has no built-in way to prevent a regular user from creating repos. If we want "view-only" accounts that can only access shared repos, we need a flag or role check in `CreateRepoHandler`.
