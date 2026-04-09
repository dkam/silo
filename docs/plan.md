# Seafile Server - Go Migration Plan

## Goal

Eliminate the C server (seaf-server) and Python web layer (Seahub) by extending
the existing Go fileserver into a standalone, single-binary server. Build a TUI
client that talks to it over HTTP.

## Architecture Target

```
TUI (or any HTTP client)
    |
    | HTTP (port 8082)
    v
Go fileserver (single binary)
    |
    v
MySQL + Filesystem
```

No RPC. No C. No Python. One server, one protocol.

## Phase 1: Cut the Cord (Make Go fileserver standalone)

The Go fileserver currently can't run without the C server because of 3 searpc
calls. Move these into Go natively.

### 1a. Access Token Store
- Add `tokenstore` package with `sync.Map` + TTL goroutine
- Struct: `{RepoID, ObjID, Op, User, ExpireTime, OneTime}`
- Token: UUID string, 1-hour TTL, cleanup every 5 minutes
- Replace `parseWebaccessInfo()` in fileop.go to use local store
- Add HTTP endpoints:
  - `POST /api/v1/access-tokens` - create token (requires auth)
  - `GET /api/v1/access-tokens/{token}` - validate (internal use)
  - `DELETE /api/v1/access-tokens/{token}` - revoke

### 1b. Decrypt Key Cache
- Add `keycache` package with `sync.Map` + TTL goroutine
- Key: "repo_id:username", Value: `{Key []byte, IV []byte, Version int}`
- 1-hour TTL, reaper every 60 seconds
- Replace `parseCryptKey()` in fileop.go to use local cache
- Add HTTP endpoint:
  - `POST /api/v1/repos/{id}/password` - set password, derive + cache key

### 1c. Event System
- Replace RPC `publish_event` with local Go channels
- For now, just log events (no consumer yet)
- Can add WebSocket/SSE endpoint later if needed

### 1d. Remove searpc dependency
- Delete `fileserver/searpc/` package
- Remove `rpcclient` global from fileserver.go
- Remove `rpcClientInit()` call and `-p` flag

**Files to modify:**
- `fileserver/fileserver.go` - remove RPC init, add new route registration
- `fileserver/fileop.go` - replace `parseWebaccessInfo()`, `parseCryptKey()`
- `fileserver/sync_api.go` - replace `publish_event` calls

**New files:**
- `fileserver/tokenstore/tokenstore.go`
- `fileserver/keycache/keycache.go`

## Phase 2: Auth Endpoint (Let clients log in)

### 2a. User Authentication
- Add `POST /api/v1/auth/login` endpoint
- Validate email + password against `EmailUser` table in ccnet DB
- Support all password formats:
  - PBKDF2SHA256 (current) - use Go's `crypto/pbkdf2`
  - Argon2id - use `golang.org/x/crypto/argon2`
  - Legacy SHA256+salt - `crypto/sha256` with fixed salt
  - Legacy SHA1 - `crypto/sha1`
- Return JWT session token (use existing golang-jwt dependency)
- JWT signed with HS256 using existing `JWT_PRIVATE_KEY` config

### 2b. Auth Middleware
- Bearer token middleware for `/api/v1/` routes
- Validate JWT, extract user email
- Inject user into request context

**New files:**
- `fileserver/authmgr/authmgr.go` - password validation + JWT
- `fileserver/middleware/auth.go` - Bearer token middleware

## Phase 3: Management API (Repo operations)

Add REST endpoints for the most-needed repo operations. Incrementally replace
what the 174 C RPC handlers do.

### Priority order (by what a TUI needs first):
1. `GET /api/v1/repos` - list user's repos
2. `GET /api/v1/repos/{id}` - repo details
3. `POST /api/v1/repos` - create repo
4. `DELETE /api/v1/repos/{id}` - delete repo
5. `GET /api/v1/repos/{id}/dir/` - list directory
6. `GET /api/v1/repos/{id}/file/` - file metadata
7. `POST /api/v1/repos/{id}/file/` - upload file
8. `DELETE /api/v1/repos/{id}/file/` - delete file

### Later:
- Sharing (add/remove/list shares)
- User management (list/create/delete users)
- Quota management
- Trash / restore
- History / revisions

**Reuse existing Go code:**
- `repomgr.Get()`, `repomgr.GetRepoOwner()` - repo queries
- `share.CheckPerm()` - permission checks
- `fsmgr` - directory/file listing
- `commitmgr` - commit operations
- `blockmgr` - block storage

## Phase 4: TUI Client

Build separately. Talks to the Go server over HTTP. Could be Go or Ruby.

## Key Constraints

- **Client compatibility**: Desktop/mobile Seafile clients must keep working.
  The existing HTTP sync API (/repo/{id}/block, /repo/{id}/commit, etc.) must
  not change.
- **Data compatibility**: Must read/write the same DB schema and filesystem
  layout. No migrations.
- **Password compatibility**: Must validate all existing password hash formats.
- **Encryption compatibility**: AES-CBC (v1,2,4) and AES-128-ECB (v3) must
  match existing client expectations.
