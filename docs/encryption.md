# Plan: Create encrypted libraries from the Silo TUI

## Context

Silo is a Go rewrite of Seafile's server. Its sync protocol, object store, and
database schema are wire-compatible with the upstream Seafile desktop and
SeaDrive clients. Encrypted-library *reading* is already plumbed through
(`fileserver/crypt.go`, `fileserver/keycache/`, `SeaDriveDownloadInfoHandler`
already surfaces `magic`/`random_key`/`salt`/`enc_version`, and
`repomgr.RepoToCommit` already knows how to serialize encryption fields into a
commit), but **creation is not implemented anywhere**:

- `repomgr.CreateRepo` (`fileserver/repomgr/repomgr.go:879`) hardcodes
  `is_encrypted=0` and has no password parameter.
- Neither `/api/silo/v1/repos` nor `/api2/repos/` accepts a password.
- Upstream Seafile only ever created encrypted libraries through **Seahub**
  (the Python/Django web UI), which does the crypto in JavaScript in the
  browser. Silo has no Seahub and no web UI, so there is no path at all today
  to get an encrypted repo into a fresh Silo install.
- The native Seafile desktop client and SeaDrive do **not** offer a
  "create encrypted library" flow — they only sync or unlock existing ones.

The natural place to add creation is the Silo TUI (`internal/tui/tui.go`),
which already owns the "press `n` to make a library" flow. We will do the
key-derivation client-side in the TUI (the password never leaves that
process), POST the derived fields to a new server endpoint, and have the
server write an `enc_version=4` encrypted repo into the DB + initial commit in
the exact format upstream Seafile desktop/SeaDrive expect. Sync clients will
then be able to pull, unlock, and sync the library the same way they would
against an upstream server.

Scope explicitly excluded: browsing/unlocking encrypted repos from the TUI
itself (the README already calls this out as a gap and it isn't what was
asked for), password-change, and the `set-password` endpoint. Those are
follow-ups — see "Out of scope / follow-ups" at the bottom.

## Wire format we must match (enc_version=4)

Authoritative reference: `server/common/seafile-crypt.c` (upstream C, still in
the tree for reference). Values below are for `enc_version=4`, the current
default used by Seafile 7.0+:

| Field        | How it's computed                                                                                              | Encoding         |
|--------------|----------------------------------------------------------------------------------------------------------------|------------------|
| `salt`       | 32 random bytes from a CSPRNG                                                                                  | 64 hex chars     |
| `key` (32B)  | `PBKDF2-HMAC-SHA256(password, salt_bin, iter=1000, dkLen=32)`                                                  | raw, not stored  |
| `iv`  (16B)  | `PBKDF2-HMAC-SHA256(key, salt_bin, iter=10, dkLen=16)`                                                         | raw, not stored  |
| `random_key` | 32 random bytes (the real file key), encrypted with `AES-128-ECB` using `key[:16]` + PKCS#7 pad → 48 bytes     | 96 hex chars     |
| `magic`      | `PBKDF2` exactly as above but over the string `repo_id + password` → 32 byte `key`                             | 64 hex chars     |
| `enc_version`| `4`                                                                                                            | integer          |
| `encrypted`  | `"true"` (string, in the commit JSON)                                                                          | string           |

Notes / gotchas, all verified from `common/seafile-crypt.c` and confirmed
against the existing Go decrypt path in `fileserver/crypt.go`:

- v4 cipher is **AES-128-ECB**. The PBKDF2 output is 32 bytes but only the
  first 16 are used as the AES key — this is the `to16Bytes` slice already
  present at `fileserver/crypt.go:82`. The existing `seafileCrypt.encrypt`
  with `version=3` handles ECB + PKCS#7 pad and can be reused verbatim for v4
  (v3 and v4 are crypto-identical; v4 is just a newer labelling).
- `magic`'s derivation re-runs the same PBKDF2 over `repo_id + password` (not
  just `password`). Client must know the repo UUID *before* computing magic,
  so the TUI generates the UUID locally and sends it.
- `random_key` is stored in the commit JSON under the key `"key"` (already
  wired in `commitmgr.Commit.RandomKey` at `fileserver/commitmgr/commitmgr.go:37`).
- `is_encrypted` in `RepoInfo` is an integer `0`/`1`, but the commit JSON
  stores `encrypted` as the **string** `"true"`. `RepoToCommit`
  (`fileserver/repomgr/repomgr.go:173`) already handles that.

## Design overview

```
TUI (cmd 'n' on repo list)
  │  name, password, encrypted? toggle
  │  locally: uuid.New() → repo_id
  │            PBKDF2/AES-ECB → salt, random_key, magic
  ▼
client.APIClient.CreateEncryptedRepo(name, repoID, encVersion, magic, randomKey, salt)
  │  POST /api/silo/v1/repos  {name, repo_id, enc_version, magic, random_key, salt}
  ▼
api.CreateRepoHandler
  │  if enc fields present → repomgr.CreateEncryptedRepo(...)
  │  else                   → repomgr.CreateRepo(...)   (existing path)
  ▼
repomgr.CreateEncryptedRepo
  │  build *Repo with IsEncrypted=true + enc fields
  │  commitmgr.NewCommit(...) ; RepoToCommit(repo, commit)  ← already exists
  │  commitmgr.Save(commit)
  │  INSERT INTO Repo / Branch / RepoHead / RepoOwner
  │  INSERT INTO RepoInfo (..., is_encrypted=1, ...)
  ▼
repoID returned
```

No schema changes: `RepoInfo.is_encrypted` and the commit-JSON fields are
already sufficient, because `SeaDriveDownloadInfoHandler`
(`fileserver/api/seadrive.go:216`) already reads `magic`/`random_key`/`salt`
/`enc_version` off the `*Repo` returned by `repomgr.Get`, and `Get`
reconstitutes them from the commit via `CommitToRepo`
(`fileserver/repomgr/repomgr.go:140`).

## Changes, file by file

### 1. New file: `fileserver/repomgr/enc.go`

Self-contained helpers for generating the encryption parameters for a new
repo. Keeping them in `repomgr` (not `fileserver/crypt.go`, which is inside
`package silod`) avoids an import cycle — the TUI/client cannot import
`silod` but can import `repomgr` or a new leaf package.

Actually, to keep both the server and the TUI honest about the wire format,
put these in a **new leaf package** `internal/seafilecrypt` that both the
server (`repomgr`) and the TUI (`internal/tui`) can import. This is the
single source of truth for the magic/random_key/salt derivation. It must have
zero non-stdlib deps beyond `crypto/*` and `golang.org/x/crypto/pbkdf2` (add
to `go.mod` if not already present).

New file: `internal/seafilecrypt/seafilecrypt.go`

```go
package seafilecrypt

// EncParams is the set of values a newly-created encrypted repo must publish
// to clients (server DB / commit JSON / /api2 download-info response).
// All string fields are lowercase hex.
type EncParams struct {
    EncVersion int    // 4
    Salt       string // 64 hex chars (32 bytes)
    Magic      string // 64 hex chars (32 bytes)
    RandomKey  string // 96 hex chars (48 bytes, encrypted 32-byte file key)
}

// DeriveV4 runs PBKDF2-HMAC-SHA256(in, salt, 1000, 32) and returns the 32-byte
// key plus a 16-byte IV derived by rerunning PBKDF2 over the key itself with
// 10 iterations. Matches seafile_derive_key() for enc_version 3/4.
func deriveV4(in []byte, salt []byte) (key [32]byte, iv [16]byte) { ... }

// GenerateV4 creates a fresh set of EncParams for a repo with the given
// (already-allocated) repoID and user password. The repoID is needed because
// upstream's magic is PBKDF2(repo_id+password, salt), not PBKDF2(password, salt).
func GenerateV4(repoID, password string) (*EncParams, error) {
    // 1. salt_bin = 32 random bytes
    // 2. key/iv   = deriveV4([]byte(password), salt_bin)
    // 3. fileKey  = 32 random bytes              ← the "real" file key
    // 4. random_key = AES-128-ECB encrypt(fileKey, key[:16]) + PKCS#7 pad  → 48 bytes
    // 5. magicKey, _ = deriveV4([]byte(repoID+password), salt_bin)
    // 6. magic = hex(magicKey[:])
    // return EncParams{4, hex(salt_bin), magic, hex(random_key)}
}
```

Reference implementations to borrow from:
- `common/seafile-crypt.c:40-89` — `seafile_derive_key`
- `common/seafile-crypt.c:91-105` — `seafile_generate_repo_salt`
- `common/seafile-crypt.c:107-137` — `seafile_generate_random_key`
- `common/seafile-crypt.c:139-158` — `seafile_generate_magic`
- `fileserver/crypt.go:15-40` — Go AES-ECB PKCS#7 encrypt (reuse the algorithm,
  not the `seafileCrypt` type, which is in `package silod`)

Unit tests (`internal/seafilecrypt/seafilecrypt_test.go`) should include a
**known-answer vector** captured from a real upstream Seafile run against a
fixed salt + password + repo_id. This is the cheapest way to prove we haven't
silently diverged from the wire format. One test per primitive
(`deriveV4`, `GenerateV4`) and one round-trip test that decrypts the generated
`random_key` back to the original 32 random bytes using
`fileserver/crypt.go`'s existing decrypt path.

### 2. `fileserver/repomgr/repomgr.go`

Add, next to the existing `CreateRepo` at line 879:

```go
// CreateEncryptedRepo creates a new encrypted repository with the given
// pre-computed encryption parameters. The caller (API handler) is responsible
// for generating repoID, salt, magic, and random_key — typically by calling
// internal/seafilecrypt.GenerateV4. The password itself is never sent to or
// stored by the server.
func CreateEncryptedRepo(repoID, name, owner string, enc *EncFields) error { ... }
```

where `EncFields` is a small struct `{EncVersion int; Magic, RandomKey, Salt string}`
defined in the same file. Implementation mirrors `CreateRepo` but:

1. Builds `*Repo` with `IsEncrypted=true`, `EncVersion=enc.EncVersion`,
   `Magic/RandomKey/Salt` set, `Version=1`.
2. Calls `commitmgr.NewCommit(...)` then `RepoToCommit(repo, commit)` (the
   existing function at `repomgr.go:173` already does the right thing for
   v4 because of the `EncVersion == 4` branch at lines 187–190 — **verify
   that this branch also sets `commit.Encrypted = "true"`, which it does
   via the earlier `commit.Encrypted = "true"` at the top of the `if
   repo.IsEncrypted {` block**).
3. `commitmgr.Save(commit)`.
4. Inserts `Repo`, `Branch`, `RepoHead`, `RepoOwner` rows (identical to
   `CreateRepo`).
5. Inserts `RepoInfo` with `is_encrypted=1` (the one line that differs from
   `CreateRepo` at `repomgr.go:912`).

Accepting a caller-supplied `repoID` is necessary because `magic` binds the
password to that exact UUID — the UUID must exist before the crypto runs.
`CreateRepo` should be refactored to accept an optional repoID too (or a
small private helper `createRepoWithID` can back both). Prefer the helper to
avoid changing `CreateRepo`'s public signature.

### 3. `fileserver/api/api.go`

Extend `createRepoRequest` (currently at line 204) to include the encryption
fields as **optional** strings:

```go
type createRepoRequest struct {
    Name       string `json:"name"`
    RepoID     string `json:"repo_id,omitempty"`      // required iff encrypted
    EncVersion int    `json:"enc_version,omitempty"`
    Magic      string `json:"magic,omitempty"`
    RandomKey  string `json:"random_key,omitempty"`
    Salt       string `json:"salt,omitempty"`
}
```

In `CreateRepoHandler` (line 213): if `req.EncVersion != 0` (or if any of
`magic/random_key/salt` are non-empty), validate that:

- `req.EncVersion == 4` — only v4 accepted for now; reject others with 400.
- `req.RepoID` is a valid UUID.
- `req.Magic` is 64 hex chars, `req.Salt` is 64 hex chars,
  `req.RandomKey` is 96 hex chars.
- `req.Name != ""`.

On success call `repomgr.CreateEncryptedRepo(...)`; on the unencrypted path
fall through to the existing `repomgr.CreateRepo(req.Name, user)`. Response
shape is unchanged — still `createRepoResponse{ID, Name}`, and the client
already knows the repo ID it generated.

### 4. `client/client.go`

Add a second entry point next to `CreateRepo` at line 178:

```go
func (c *APIClient) CreateEncryptedRepo(
    repoID, name string, enc seafilecrypt.EncParams,
) (*Repo, error) {
    body := map[string]any{
        "name":        name,
        "repo_id":     repoID,
        "enc_version": enc.EncVersion,
        "magic":       enc.Magic,
        "random_key":  enc.RandomKey,
        "salt":        enc.Salt,
    }
    var repo Repo
    err := c.doRequest("POST", "/api/silo/v1/repos", body, &repo)
    ...
}
```

`CreateRepo` (unencrypted) stays as-is for backwards compatibility with
everything else that calls it.

### 5. `internal/tui/tui.go`

Changes, all localised to the `viewNewRepo` screen (keybinding at line 308,
update at 390–423, render at 425–435). The login view's two-field pattern is
the template to copy — it already toggles focus between `emailInput` and
`passwordInput` (an `EchoMode = textinput.EchoPassword` field) with Tab/Shift-
Tab, and it's the simplest existing example.

Model additions (around lines 66–115):

```go
newRepoPasswordInput textinput.Model   // masked
newRepoEncrypted     bool              // toggle with 'e' or space
newRepoFocus         int               // 0=name, 1=password
```

`initialModel()` (around line 128) creates the masked input exactly like the
login password.

`updateNewRepo`:

- `tab` / `shift+tab`: toggle `newRepoFocus` between 0 and 1, but only when
  `newRepoEncrypted` is true (otherwise there's no password field to focus).
- `ctrl+e` (or another unused key — **not** `e`, which users will type into
  the name field): toggle `newRepoEncrypted`. When toggled on, focus the
  password input; when off, clear and unfocus it.
- `enter`:
  - If `!newRepoEncrypted`: unchanged — call `m.api.CreateRepo(name)`.
  - If `newRepoEncrypted`:
    1. Validate name and password are non-empty.
    2. `repoID := uuid.New().String()`
    3. `enc, err := seafilecrypt.GenerateV4(repoID, password)` — local, fast.
    4. Return a `tea.Cmd` that calls
       `m.api.CreateEncryptedRepo(repoID, name, *enc)`.
    5. Wipe the password input immediately after kicking off the command so
       it doesn't linger in the model beyond the time it takes to derive the
       params. (The password never touches the network.)

Reuse the existing `repoCreatedMsg` (line 49) for the completion path. Reuse
the existing `errorStyle` / `successStyle` / `m.message` pattern for
feedback.

`renderNewRepo`: show "Create Library", the name input, a line
`[ ] encrypted  (ctrl+e to toggle)` / `[x] encrypted`, and — when encrypted is
on — the masked password input below. Help text becomes
`enter: create  tab: switch field  ctrl+e: toggle encryption  esc: cancel`.

After a successful encrypted create, the TUI returns to the repo list the
same way it does today. The new repo appears with the existing
`[encrypted]` dim label (already handled at `tui.go:372`). The TUI
**cannot** open/browse it — attempting to press `enter` on it should show a
clear error message "encrypted libraries can only be synced by a desktop
client" rather than failing somewhere deeper. Check `updateRepos`' enter
handler to add this guard.

## Critical files

- `internal/seafilecrypt/seafilecrypt.go` (new) + test
- `fileserver/repomgr/repomgr.go` — add `CreateEncryptedRepo`, reuse `RepoToCommit`
- `fileserver/api/api.go` — extend `createRepoRequest` + `CreateRepoHandler`
- `client/client.go` — add `CreateEncryptedRepo`
- `internal/tui/tui.go` — new input, focus toggle, encryption toggle, enter handler, browse guard

Reference-only (do not edit, but consult):
- `fileserver/commitmgr/commitmgr.go:19-46` — `Commit` struct (encryption fields)
- `fileserver/repomgr/repomgr.go:140-199` — `CommitToRepo` / `RepoToCommit` (already enc-aware)
- `fileserver/crypt.go:15-68` — existing v3/v4 ECB encrypt/decrypt (source of the round-trip test vector)
- `fileserver/api/seadrive.go:195-259` — `SeaDriveDownloadInfoHandler` (already returns the enc fields, no change needed)
- `server/common/seafile-crypt.c:40-158` — authoritative C implementation

## Verification

### Unit level

1. `go test ./internal/seafilecrypt/...` — PBKDF2 + AES-ECB known-answer
   vectors and a round-trip test that takes the output of `GenerateV4` and
   decrypts `random_key` back to the original file key using the existing
   `fileserver/crypt.go` decrypt (version=4).
2. `go test ./fileserver/repomgr/...` — a test that calls
   `CreateEncryptedRepo`, then `repomgr.Get(repoID)`, and asserts
   `IsEncrypted`, `EncVersion==4`, and non-empty `Magic`/`RandomKey`/`Salt`
   match what went in.
3. `go test ./fileserver/api/...` — a handler test that POSTs a valid
   encrypted-create request and one that POSTs `enc_version=2` (rejected).

### End-to-end with a real Seafile desktop client

This is the only test that actually proves wire compatibility. Steps, run
against a scratch data dir:

1. `go build ./cmd/silo && ./silo serve -F /tmp/silo-conf -d /tmp/silo-data`
2. `./silo tui http://localhost:8082` → log in with
   `SEAFILE_ADMIN_EMAIL/PASSWORD`.
3. Press `n`, enter name `enc-test`, press `ctrl+e`, enter password
   `hunter2`, press enter. Expect success and to see
   `enc-test [encrypted]` in the list.
4. In the DB: `sqlite3 /tmp/silo-data/seafile.db 'select repo_id, name, is_encrypted from RepoInfo where name="enc-test"'` —
   expect `is_encrypted=1`.
5. Inspect the initial commit on disk under
   `/tmp/silo-data/storage/commits/<repo-id>/...` and confirm the JSON has
   `"encrypted":"true","enc_version":4,"magic":"...","key":"...","salt":"..."`.
6. Point an upstream **Seafile Desktop** client at `http://localhost:8082`,
   log in as the same admin, sync the `enc-test` library, enter password
   `hunter2` when prompted. Drop a file in the synced folder, wait for
   upload, then confirm the block file under
   `/tmp/silo-data/storage/blocks/` is ciphertext (not the plaintext).
7. Stop the desktop client, delete its local cache, re-sync, re-enter the
   password, and confirm the file comes back with correct contents — this
   proves both the magic verification and the random_key unwrap work against
   our generated parameters.
8. Repeat step 6 with **SeaDrive** 3.0.21 as a second client to cover the
   `/api2/` path.

If step 6 fails at "wrong password", the bug is in either `magic` (double-
check the `repo_id+password` concat) or the PBKDF2 parameters. If it fails at
"cannot decrypt file", the bug is in the `random_key` wrap (double-check AES-
128-ECB + PKCS#7 + the `key[:16]` slice).

## Out of scope / follow-ups

These are deliberately not in this plan — they are separate, smaller changes
that can land later without blocking the creation path:

- **`/api2/repos/{id}/set-password/` handler.** Required for *server-side*
  features like web preview, thumbnails, and `parseCryptKey`
  (`fileserver/fileop.go:223`) on encrypted repos. Sync itself does not
  strictly need it, but some clients call it during the download flow —
  worth adding as the next step so both clients light up cleanly.
- **Password change.** `seaf_passwd_manager_set_passwd` in the C code does an
  unwrap/rewrap of `random_key` with the new password; porting that is
  straightforward once `set-password` exists.
- **TUI browse of encrypted repos.** Would require keeping the derived file
  key in memory in the TUI process and doing client-side block decryption
  during `silo get`. Bigger change, not needed for this task.
- **enc_version < 4 support.** We only create v4. Older repos imported from
  an upstream Seafile install will still read correctly (the decrypt path in
  `fileserver/crypt.go` already handles v2 and v3), but Silo will never
  produce them.
