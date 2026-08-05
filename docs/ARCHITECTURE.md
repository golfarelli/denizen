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

Built as a static SPA (`@sveltejs/adapter-static`, `fallback: 'index.html'`,
`ssr`/`prerender` off in the root layout) rather than with SvelteKit's own
Node server — there's no Node runtime in the deployed container to run one
(see "Deployment" below). No CSS framework either: a small hand-written
design system (`web/src/lib/styles/app.css`, CSS custom properties for
light/dark) — consistent with CONTRIBUTING.md's minimal-dependencies rule,
and the current surface (auth forms, a file browser) doesn't need more.
Both tokens from login live in `localStorage` for now (see `web/src/lib/auth.ts`)
rather than the access-token-in-memory + refresh-token-in-an-httpOnly-cookie
split noted as the eventual hardening target below (under Auth) — the
backend hands back both tokens in the JSON body, not a cookie, so this is
the as-built API shape's straightforward client-side counterpart, not the
final design.

Uploads go through [tus-js-client](https://github.com/tus/tus-js-client)
(`web/src/lib/upload.ts`) — the official JS client for the same protocol
`tusd` speaks server-side (see "Uploads" below), the same "genuinely complex
infrastructure" exception CONTRIBUTING.md already carves out for `tusd`
itself, made again for the same reason on the client. Its `onBeforeRequest`
hook re-reads the access token from the store on *every* HTTP request the
client makes, not just once at the start — a large upload can easily
outlive the access token's TTL (default 15 minutes), and that per-request
re-read (plus a 401-triggered refresh wired through `onShouldRetry`) is what
keeps a long upload from failing partway through purely because of that.

## Deployment: a single container

The Go binary embeds the built SvelteKit static assets (`go:embed`) and serves
both the API and the web app from one process, one image, one port.

`cmd/server` shuts down on `SIGINT`/`SIGTERM` (what `docker stop` sends) via
`signal.NotifyContext` and `http.Server.Shutdown`, rather than dying mid-request —
the same cancellable context also stops the two background sweeps (upload GC,
trash purge; see below) instead of leaving them running past shutdown.

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
- Trash is auto-purged after 30 days (`DENIZEN_TRASH_RETENTION`, swept every
  `DENIZEN_TRASH_PURGE_INTERVAL`, default 1h, by
  `ItemService.PurgeExpiredTrash`) and **counts against the user's quota**
  while it sits there — otherwise "deleting" would be a way to dodge the
  quota for a month without freeing real space.

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
  /shares/{id}`, and public (no-auth) `GET /s/{token}` / `GET
  /s/{token}/content`. Revocation and listing are keyed by the share's own
  `id`, not its token — a deliberate deviation from an earlier draft of this
  sketch, made once the token-hashing decision (see the database schema
  section) meant the raw token can't be shown again after creation to key
  anything by.
- **Users** (admin) — `GET /users`, `PATCH /users/{id}`
  (`quota_bytes`, `disabled`). Unlike items' PATCH (a full replacement, see
  above), this one is a genuine partial update — either field can be sent on
  its own and the other is left as it was.
- **Me** — `GET /me` (`quota_bytes`, `storage_used_bytes`, ...).

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
Incomplete/abandoned uploads (started, never finished, and untouched for
`DENIZEN_UPLOAD_GC_AFTER`, default 24h) are swept from the staging directory
on a timer (`DENIZEN_UPLOAD_GC_INTERVAL`, default 1h) by
`internal/upload.CollectGarbage`, judging age from each upload's `.info` file
mtime (tusd's `FileInfo` doesn't carry a timestamp of its own).

### Auth

JWT (access + refresh tokens). Registration is invite-only — no public
sign-up, matching a private drive for family/friends rather than a public
service.

The API itself is transport-agnostic about where the client keeps these
(it just expects an `Authorization: Bearer` header) — the frontend currently
keeps both in `localStorage` rather than the more hardened
access-token-in-memory + refresh-token-in-an-httpOnly-cookie split floated
early on; see "Frontend: SvelteKit" above for why that's an acceptable
starting point, not the final design.

### Sharing (MVP)

Per-file/folder share **links** with a random token, not a full per-user ACL
system. Simpler to build and matches the actual use case (send someone a
link, they don't need an account). A proper "shared with a specific user,
with a role" model is a plausible v2 if the need for it shows up in practice.

Only the token's **hash** is stored (same reasoning as refresh tokens — a
database leak alone shouldn't hand out live share links); the raw token is
shown exactly once, in the `Create` response, and can't be retrieved again.
That's also why revocation and the "my shares" listing are keyed by the
share's own `id` rather than the token itself (see the API sketch above).
`requires_auth` only asks "is this visitor logged in to *some* Denizen
account", not "do they own anything" — consistent with link-based sharing
rather than per-user ACLs.

## Quotas

- `users.storage_used_bytes` is an incrementally maintained counter, not a
  `du` scan on every check.
- Two checks on upload: an optimistic one at creation time (declared
  `Upload-Length` vs. remaining quota, checked in the tus pre-create hook —
  rejects before a single byte is staged), and a second one right before the
  file is actually committed in `FinalizeUpload` — so two uploads racing
  each other can't both slip past the optimistic check and overrun the quota
  together. These are two sequential checks, not one atomic database
  transaction (the repository layer doesn't have transaction support yet —
  see CONTRIBUTING.md's note on this same trade-off for invite redemption).
- A `statfs` check against real free disk space (`internal/storage.FreeBytes`)
  runs alongside the logical quota check, as a safety net against quotas
  summing to more than the disk actually has.
- New users get a configurable global default quota, adjustable per invite
  (`quota_bytes` override) and, later, per-user by an admin (the `PATCH
  /users/{id}` endpoint that would let an admin change it after the fact
  isn't built yet — see the API sketch above).

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
