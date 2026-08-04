# Architecture

This document summarizes the MVP architecture and the reasoning behind the
main decisions. It will grow and get revised as the project evolves.

## Scope of the MVP

Plain file storage: upload/download, folders, share links. No online
collaborative document editing, no desktop sync client — both are candidates
for later, once the core is solid.

## Backend: Go

I/O-bound streaming (uploads/downloads) is Go's natural habitat: lightweight
concurrency, low memory footprint, single static binary → a small container
image. That matters because Denizen is meant to run alongside several other
containers on modest home-server hardware, not on a dedicated box.

## Frontend: SvelteKit

Compiles away, no virtual DOM shipped to the client, small bundles — consistent
with a deliberately light backend.

## Deployment: a single container

The Go binary embeds the built SvelteKit static assets (`go:embed`) and serves
both the API and the web app from one process, one image, one port.

## Database: SQLite (not Postgres)

SQLite is embedded, not a server process — the entire database is one file on
disk. Backing it up is as simple as copying the data directory; there's no
`pg_dump`/`pg_restore` step and no extra container to run.

Mitigations against SQLite's known corruption risks:

- **WAL mode** enabled.
- **Single-writer discipline** in the application (one backend process).
- The data directory must live on a **local disk**, never a network filesystem
  (NFS/SMB) — SQLite's locking isn't reliable there.
- **[Litestream](https://litestream.io/)** streams continuous replication to a
  second copy, so a crash loses at most a few seconds of writes, never the
  whole file.

Alternatives considered and set aside: libSQL/Turso (built for multi-node/edge
replication, not needed for a single-node deployment), LMDB (no SQL, needs
CGO), bbolt/BadgerDB (pure key-value, would mean reimplementing relational
queries by hand), DuckDB (built for analytical workloads, not small frequent
transactions).

## Database schema

The full DDL lives in [`internal/db/schema.sql`](../internal/db/schema.sql)
(colocated with the Go code that embeds and applies it): `users`, `invites`
(the only way an account gets created), `refresh_tokens`, `items` (files and
folders, in one table, related to each other via `parent_id`), and `shares`.

## Storage layout

```
/data
├── db/
│   ├── denizen.db          (SQLite, WAL mode)
│   ├── denizen.db-wal
│   └── denizen.db-shm
├── users/
│   ├── <username>/
│   │   ├── files/           # mirrors the user's visible folder tree
│   │   └── .trash/          # flat, "<id>_<original name>", auto-purged after 30 days
│   └── ...
└── staging/
    └── uploads/              # in-progress tus chunks — same filesystem as users/
```

- Files are real files on disk, mirroring the user's visible folder structure
  1:1 — the data stays inspectable and recoverable without the app even
  running (`ls`/`scp` still work). A folder rename/move does a real
  filesystem `rename()`, which is cheap (metadata-only, not a recursive copy).
- The API and database still identify every file/folder by a stable ID (see
  below) — the on-disk path is a derived, kept-in-sync property, not the
  source of truth.
- Filename collisions in the same folder get an auto-suffix (`file (1).pdf`),
  the same behavior as a normal desktop file manager.
- Trash is auto-purged after 30 days and **counts against the user's quota**
  while it sits there — otherwise "deleting" would be a way to dodge the quota
  for a month without freeing real space.

## API

REST/JSON under `/api/v1`, with a uniform error envelope (`{error: {code,
message}}`) and consistent HTTP status codes.

**Resources are identified by a stable ID (UUID), not by path.** Renaming or
moving a file/folder never changes its ID, so share links, trash entries, and
any other reference to it stay valid. Folder structure is expressed with a
`parent_id` field. This mirrors how Google Drive's and Dropbox's own APIs
model files.

Sketch of the main endpoints:

- **Auth** — `/auth/login`, `/auth/refresh`, `/auth/logout`, `/auth/register`
  (invite-only, requires an `invite_code`).
- **Invites** — `POST /invites` (admin only).
- **Items** — `GET/POST /items`, `GET/PATCH /items/{id}`, `DELETE /items/{id}`
  (soft delete → trash).
- **Trash** — `GET /trash`, `POST /items/{id}/restore`, `DELETE /trash/{id}`
  (permanent).
- **Content** — `GET /items/{id}/content` (streamed, supports `Range`).
- **Uploads** — resumable, chunked, via the [tus protocol](https://tus.io/) at
  `/uploads` (see below).
- **Shares** — `POST /items/{id}/shares`, `GET /shares`, `DELETE
  /shares/{token}`, and public (no-auth) `GET /s/{token}` / `GET
  /s/{token}/content`.
- **Users** (admin) — `GET /users`, `PATCH /users/{id}`
  (`quota_bytes`, `disabled`).
- **Me** — `GET /me` (`quota_used`, `quota_total`).

### Uploads: resumable, chunked (tus)

A plain single-request multipart upload restarts from zero on any dropped
connection — a real cost on a mobile network, especially for the built-in
document scanner producing large PDFs from outside the house. The
[tus protocol](https://tus.io/) resumes from the last acknowledged byte
instead.

`tusd` runs embedded in the Go binary (as a library, not a separate process —
consistent with the single-container deployment), backed by a local filestore
rooted at `<data>/staging/uploads` (see the storage layout above — this must
be on the same filesystem as `users/`, since finishing an upload is a
`Rename`, not a copy). Flow: `POST` to create an upload (`Upload-Length` /
`Upload-Metadata` headers carry `filename`, `parent_id`, optionally
`filetype`) → `PATCH` the bytes with `Upload-Offset`, resumable via `HEAD`
after an interruption.

The whole `/api/v1/uploads/` subtree sits behind the same JWT auth middleware
as everything else — tusd's own hooks only cover create/finish/terminate, not
the `PATCH`/`HEAD`/`GET` requests that continue or read back an upload, so the
middleware is what actually closes that gap. On top of that, a `PreUploadCreateCallback`
hook validates the destination folder up front and stamps the upload's
metadata with the *server-verified* owner ID (never trusting whatever the
client claims), and a `PreFinishResponseCallback` hook — which runs
synchronously, before the completing request gets its response — checks that
owner ID still matches the caller, then moves the finished file into the
user's real folder tree, computes its checksum, registers it in the `items`
table, and adjusts `storage_used_bytes`. The client learns the new item's ID
immediately via an `X-Item-Id` response header, no separate listing needed.
Incomplete/abandoned uploads being garbage collected on a schedule is not yet
implemented — a follow-up, not silently skipped.

### Auth

JWT (access + refresh tokens). Registration is invite-only — no public
sign-up, matching a private drive for family/friends rather than a public
service.

### Sharing (MVP)

Per-file/folder share **links** with a random token, not a full per-user ACL
system. Simpler to build and matches the actual use case (send someone a
link, they don't need an account). A proper "shared with a specific user,
with a role" model is a plausible v2 if the need for it shows up in practice.

## Quotas

- `users.storage_used_bytes` is an incrementally maintained counter, not a
  `du` scan on every check.
- Two checks on upload: an optimistic one at creation time (declared
  `Upload-Length` vs. remaining quota), and an authoritative one inside the
  same transaction that finalizes the upload and increments the counter — so
  two concurrent uploads can't both slip past the optimistic check and
  overrun the quota together.
- A `statfs` check against real free disk space runs independently of the
  logical quota, as a safety net against quotas summing to more than the disk
  actually has.
- New users get a configurable global default quota, adjustable per-user
  afterwards by an admin.

## Mobile: Android PWA

Manual browsing/upload/download rather than an automatic camera-roll backup
(that's already covered on this stack by a separate photo-management tool).
Two things worth calling out:

- **Document scanner**, entirely in the browser: `getUserMedia` for camera
  access, edge detection + perspective correction client-side, pages
  assembled into a PDF before upload — no native app needed.
- **Web Share Target API** so other apps' "Share" menu can hand files
  straight to Denizen. Android/Chrome only for now (iOS Safari doesn't
  implement it for PWAs) — acceptable since Android is the only mobile target
  for this MVP.
