# Silo

A single-binary Go file sync server, protocol-compatible with Seafile clients.

## What is Silo?

Silo is a Go rewrite of the Seafile server architecture. Where upstream Seafile ships a C daemon (`seaf-server`), a Python/Django web layer (Seahub), and a process manager to tie them together, Silo collapses all of that into a single Go binary that speaks HTTP directly and talks directly to its database.

It keeps full wire compatibility with existing Seafile clients. The sync protocol, block storage layout, and database schema are unchanged, so Seafile desktop, mobile, and SeaDrive clients work against Silo without modification.

Silo also ships with `silo`, a terminal UI built on [Bubble Tea](https://github.com/charmbracelet/bubbletea) for interactive file management without a browser.

## Architecture

```
  Client (TUI / SeaDrive / Seafile Desktop)
              │
              │ HTTP :8082
              ▼
         ┌──────────┐
         │   Silo   │   single Go binary
         └────┬─────┘
              │
        ┌─────┴─────┐
        ▼           ▼
   SQLite or    Filesystem
   MySQL        (content-addressable
                blocks / commits / fs)
```

- One process. No RPC, no Python, no controller.
- Two logical databases — `ccnet` (users, groups) and `seafile` (repos, shares, tokens). Both live in a SQLite file or a MySQL schema.
- Content-addressable object store under `{data-dir}/storage/` with separate trees for blocks, commits, and filesystem objects.

## Features

- Single-admin bootstrap via environment variables
- JWT session tokens for the management API
- Persistent API tokens for SeaDrive compatibility
- Repo create / list / delete
- File operations: upload, download, mkdir, rename, move, delete
- Directory listing via `/api/v1/repos/{id}/dir/`
- Full Seafile sync protocol for desktop and SeaDrive clients
- SQLite (embedded, WAL mode, read/write connection split) or MySQL backend
- Auto-generated ephemeral JWT signing key if `JWT_PRIVATE_KEY` is unset
- Seafile-compatible endpoints: `/api2/auth-token/`, `/api2/repos/`, `/api2/repos/{id}/repo-tokens/`, `/api2/repos/{id}/download-info/`, plus the full sync path

## Quick start

### Run the server (SQLite)

```bash
cd fileserver
go build .

mkdir -p /tmp/silo-data
SEAFILE_DB_TYPE=sqlite \
SEAFILE_ADMIN_EMAIL=admin@example.com \
SEAFILE_ADMIN_PASSWORD=changeme \
SEAFILE_LOG_TO_STDOUT=1 \
./fileserver -F /tmp/silo-data -d /tmp/silo-data
```

The server listens on `:8082`. On first run it creates the ccnet and seafile SQLite databases, the storage directory, and the admin user.

### Run the TUI client

In another terminal:

```bash
cd cmd/silo
go build .

SEAFILE_URL=http://localhost:8082 \
SEAFILE_EMAIL=admin@example.com \
SEAFILE_PASSWORD=changeme \
./silo
```

Auto-login kicks in if both `SEAFILE_EMAIL` and `SEAFILE_PASSWORD` are set. From there: `n` to create a library, `enter` to open it, `u` to upload a local file, `v` to move, `r` to rename, `x` to delete, `q` to quit.

## Configuration

### Environment variables

| Variable | Purpose | Default |
|---|---|---|
| `SEAFILE_FILESERVER_HOST` | Bind address | from `seafile.conf` |
| `SEAFILE_FILESERVER_PORT` | Listen port | `8082` |
| `SEAFILE_DB_TYPE` | `sqlite` or `mysql` | from `seafile.conf` |
| `SEAFILE_ADMIN_EMAIL` | Create admin user on startup | — |
| `SEAFILE_ADMIN_PASSWORD` | Admin password | — |
| `JWT_PRIVATE_KEY` | Session token signing key | auto-generated (ephemeral) |
| `SEAFILE_LOG_TO_STDOUT` | Write logs to stdout instead of file | unset |
| `SEAFILE_MYSQL_DB_HOST` | MySQL host | from `seafile.conf` |
| `SEAFILE_MYSQL_DB_PORT` | MySQL port | `3306` |
| `SEAFILE_MYSQL_DB_USER` | MySQL user | from `seafile.conf` |
| `SEAFILE_MYSQL_DB_PASSWORD` | MySQL password | from `seafile.conf` |
| `SEAFILE_MYSQL_DB_CCNET_DB_NAME` | ccnet database name | from `seafile.conf` |
| `SEAFILE_MYSQL_DB_SEAFILE_DB_NAME` | seafile database name | from `seafile.conf` |

Env vars take precedence over `seafile.conf`, so the same binary can be pointed at different deployments without editing files.

### CLI flags

| Flag | Purpose |
|---|---|
| `-F <dir>` | Central config directory (contains `seafile.conf`) |
| `-d <dir>` | Data directory (storage, SQLite files, logs) |
| `-l <file>` | Log file path (ignored if `SEAFILE_LOG_TO_STDOUT` is set) |
| `-P <file>` | PID file path |
| `-debug` | Log every HTTP request |

## Client compatibility

Silo has been tested with:

- **Silo TUI** (`cmd/silo`) — full CRUD and browse
- **SeaDrive** 3.0.21 — sync and file operations via `/api2/` endpoints
- **Seafile Desktop** — sync via the standard repo token protocol

The JWT management API (`/api/v1/`) is new and Silo-specific; existing Seafile clients don't know about it.

## What's not implemented

Silo is a lean rewrite focused on the sync path and a minimal management API. The following upstream Seafile features are **not** available:

- No user management API — users are created via `SEAFILE_ADMIN_EMAIL`/`SEAFILE_ADMIN_PASSWORD` or direct database insert
- No repo sharing API — users can only access repos they own (share tables exist in the schema but have no HTTP endpoints)
- No group management API
- No `is_staff` / admin privilege check in the API layer — all authenticated users have equal permissions
- No web UI — use the TUI or a Seafile client
- No notification server — SeaDrive falls back to polling
- No trash / restore or history / revision endpoints
- No encrypted-repo support in the TUI (sync clients can still use encrypted repos)

See [`docs/future-features.md`](docs/future-features.md) for the rough roadmap.

## Directory layout

```
fileserver/        Active Go server
  ├── api/         Management API handlers (/api/v1/*)
  ├── authmgr/     Password validation + JWT
  ├── dbutil/      SQLite / MySQL abstraction, portable upsert helpers
  ├── share/       Permission checking
  ├── tokenstore/  In-memory access token cache
  ├── keycache/    In-memory decrypt key cache
  └── ...
cmd/silo/          Bubble Tea TUI client
docs/              Architecture notes, migration plan, future features
server/            Legacy C seaf-server code — not built, kept for reference
python/            Legacy Seahub code — not used
```

## Origin and license

Silo started as a fork of [haiwen/seafile-server](https://github.com/haiwen/seafile-server). It reuses the on-disk format, database schema, and wire protocol so that upstream clients keep working.

Licensed under **AGPLv3**, inherited from the upstream project. See [`NOTICE`](NOTICE) for attribution and [`LICENSE.txt`](LICENSE.txt) for the full license text.
