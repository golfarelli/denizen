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

Tested end-to-end with Playwright (`web/e2e/`), against the actual `denizen`
binary and a real, disposable SQLite database — not `npm run dev`'s proxy
setup, and nothing mocked — the browser-side counterpart to the backend's
own flow tests. See CONTRIBUTING.md's Testing section.

**File preview** (`routes/file/[id]/+page.svelte`) — tapping a file opens it
here instead of only ever offering a download, for images, PDFs, video,
Word/Excel documents, and text (`text/*`, plus a small hardcoded extension
list — `.md`, `.json`, `.log`, etc. — for cases Go's `mime.TypeByExtension`
doesn't reliably know about). Anything else (PowerPoint, legacy `.doc`,
audio, ...) falls back to a "preview not available" message with a
Download button, the same way Google Drive itself degrades for a type it
can't render. Every heavy viewer (pdf.js, docx-preview, xlsx) is loaded via
a dynamic `import()`, not a static one at the top of the file — together
they're a genuinely large ~290KB (gzipped) payload, and bundling all three
into this route unconditionally would mean paying for it even to open a
text file or an image.

The preview page also runs in **fullscreen mode**: on mount it flips a
shared `lib/fullscreen.ts` store, which `+layout.svelte` uses to hide the
app's own topbar and drop `<main>`'s centered reading width, and reverts on
unmount. The page's own chrome collapses into one slim header row (back
arrow, truncated title, download button) instead of a separate breadcrumb
and toolbar, so every viewer below — OnlyOffice above all, which is
otherwise cramped on a phone even at the right `type` (see below) — gets the
real remaining viewport height (`main.fullscreen` sizes off `100dvh`, not
`100vh`, so a mobile browser's address bar showing/hiding doesn't clip it).

Two different ways of getting bytes into a viewer, chosen per format:

- **Images, PDF, Word, Excel, text**: `<img>`/pdf.js/docx-preview/xlsx all
  either can't carry the `Authorization` header the content endpoint needs
  (`<img>`) or just want the whole file up front to parse anyway (the other
  three), so the bytes are fetched once as a blob (same constraint, same
  fix, as the download button) and handed over as a `blob:` URL, a `Blob`,
  or in-memory text — not streamed via `Range` requests.
- **Video**: a real `<video src>` instead, backed by a short-lived
  *content token* (`internal/token.ContentClaims`, minted by `POST
  .../content-token`, checked by
  `middleware.RequireAuthOrContentToken`) passed in the URL's query string
  rather than a header — the one case among these where downloading the
  whole file before showing anything would be a real, felt cost (starts
  playing immediately and seeks around without downloading all of it
  first, the same as Drive's own video preview, instead of a multi-hundred-
  MB wait up front). The token is checked against the item id in the URL
  it's presented on, so even a leaked one is useless for anything but that
  one file, for at most an hour (`contentTokenTTL`,
  `internal/handler/item.go`) — deliberately more generous than the access
  token's own default 15-minute TTL, since cutting a video short mid-watch
  would be a worse tradeoff than that narrow extra exposure window.

PDFs render through **pdf.js** (`lib/PdfViewer.svelte`), not an `<iframe
src="blob:...">` — that was the first approach, and it does work on
desktop, but silently shows a blank pane on Android Chrome (real hardware,
confirmed — the browser's own inline PDF viewer just doesn't reliably
activate for a blob: URL embedded in an iframe on that platform). pdf.js
sidesteps the platform's native PDF support entirely by parsing the file
itself and rendering every page to its own `<canvas>` — consistent across
every browser instead of depending on each one's own plugin behavior.

Word/Excel render through **docx-preview** and **xlsx** (SheetJS)
respectively (`lib/DocxViewer.svelte`, `lib/XlsxViewer.svelte`) — real
dependencies, not hand-rolled, unlike the PDF *writer* the camera scanner
uses (see "Mobile: Android PWA" below — generating a simple image-per-page
PDF is a small, bounded problem; parsing arbitrary Office/PDF byte streams
for rendering is not, squarely the "genuinely complex infrastructure"
category CONTRIBUTING.md already carves exceptions for, same reasoning as
`tusd`). No PowerPoint viewer and no legacy binary `.doc` — docx-preview
only reads the modern OOXML format; `xlsx`'s bundled legacy parser does
cover old-style `.xls`, so that one's supported alongside `.xlsx`.
`xlsx`'s own npm package has two open CVEs SheetJS never backported a fix
for there (they publish patched releases only from their own CDN now) — installed
from `cdn.sheetjs.com` directly instead of the npm registry for that reason
(see `web/package.json`). The spreadsheet itself renders as a plain HTML
`<table>` built from `sheet_to_json`'s raw cell values through Svelte's own
`{expression}` interpolation, deliberately not SheetJS's own
`sheet_to_html` + `{@html}` — the latter would mean trusting that helper's
escaping to keep a malicious spreadsheet's cell contents from becoming
markup, where Svelte's normal text interpolation (a plain string, escaped
like any other untrusted text) needs no such trust at all.

### Optional: OnlyOffice for real Office fidelity (and, eventually, editing)

docx-preview/xlsx get the content across, but their rendering is
approximate — real layout/formatting fidelity, and any editing at all,
needs an actual Office-compatible engine, the same conclusion Nextcloud's
own "Nextcloud Office" integration reached. That's a genuinely different
scale of thing from a JS library: a whole second server
(`docker-compose.onlyoffice.yml`, an
[OnlyOffice Document Server](https://github.com/ONLYOFFICE/DocumentServer)
container) running its own rendering engine — hundreds of MB to 1GB+ of
image and real RAM per open document, not a dependency bump. Kept strictly
**optional**: unconfigured (`DENIZEN_ONLYOFFICE_URL` unset, the default),
`internal/onlyoffice.Client.Enabled()` reports false and both its routes
degrade gracefully (`/onlyoffice/status` reports `{enabled: false}`,
`/onlyoffice-config` 404s) rather than erroring — the frontend then falls
straight back to docx-preview/xlsx, exactly as if this whole feature didn't
exist. This matters specifically because Denizen is open source: a
self-hoster who doesn't want a second heavy container isn't paying for one
just because the code path exists.

**This is phase 1: viewing, not editing.** `EditorConfig.EditorConfig.Mode`
is hardcoded to `"view"` (`internal/handler/onlyoffice.go`) — no
`callbackUrl` is wired up, and there's no server-side handling yet for the
Document Server's save-back POST. Editing is real, substantial follow-up
work (session/lock semantics, validating and applying the callback, a UI
affordance for "editing" vs "viewing"), deliberately not bundled into this
first pass.

**How it fits together** (OnlyOffice's own config-and-callback model, not
strictly the Microsoft WOPI protocol Collabora Online uses, but the same
shape): the frontend calls `GET /items/{id}/onlyoffice-config`
(`internal/handler/onlyoffice.go`), which returns a JSON config —
document URL, file type, permissions, a JWT signature over the whole
thing — that gets handed to `DocsAPI.DocEditor`, a script loaded directly
from the Document Server itself (`lib/OnlyOfficeViewer.svelte`), not
bundled by Denizen (it has to match whatever server version is actually
running). Two URLs matter here, commonly *not* the same address:

- `DENIZEN_ONLYOFFICE_URL` — where a **browser** reaches the Document
  Server, to load its editor script and iframe.
- `DENIZEN_ONLYOFFICE_DOCUMENT_BASE_URL` — where the **Document Server
  itself** reaches Denizen, to fetch the file's actual bytes. On the
  reference compose overlay these are two different addresses (a
  published host port vs. the other container's name on their shared
  Docker network) — there's no way to safely infer one from the other, so
  both are explicit config, not derived from an incoming request's Host
  header the way that might work for a simpler single-address setup.

The document-fetch URL itself rides on the *same* content-token mechanism
`<video>` uses (`internal/token.ContentClaims` — see "File preview"
above) — the Document Server is fetching a file over plain HTTP with no
way to attach Denizen's own bearer token, exactly the problem that
mechanism already solves, so this reuses it rather than inventing a
second one. The whole config object is signed with
`DENIZEN_ONLYOFFICE_JWT_SECRET` (`internal/onlyoffice.Client.Sign`) —
OnlyOffice's own security model expects this once `JWT_ENABLED=true` is
set Document-Server-side (the reference overlay always sets it), so an
unsigned config would just be rejected.

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
  (soft delete → trash), `POST /items/{id}/copy` (real new bytes for files,
  recursive for folders — never a hard link, see `ItemService.Copy`).
- **Trash** — `GET /trash`, `POST /items/{id}/restore`, `DELETE /trash/{id}`
  (permanent).
- **Content** — `GET /items/{id}/content` (streamed, supports `Range`;
  accepts either the normal bearer token or a short-lived `?token=` content
  token minted by `POST /items/{id}/content-token` — see "File preview"
  above for why `<video>` needs the latter).
- **OnlyOffice** (optional — see "File preview" above) — `GET
  /onlyoffice/status`, `GET /items/{id}/onlyoffice-config`. Both report
  "disabled"/404 rather than erroring when no Document Server is
  configured.
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

**Not a separate client.** The same SvelteKit SPA that's already
mobile-responsive (see the kebab action menu above) is the PWA — turning it
into one meant adding a manifest, icons, and a service worker
(`web/static/manifest.json`, `web/static/sw.js`), not building anything new
from scratch. Duplicating the file browser in a second codebase was
considered and rejected: same UI, twice the maintenance, no real benefit.

**Hard requirement: a secure context.** Service worker registration itself
refuses to run outside HTTPS or `localhost` — on a plain-HTTP LAN deployment
(the minimal home-server setup this project also targets) that means
`navigator.serviceWorker` never appears, and everything below silently
doesn't work while the rest of the app is unaffected. TLS termination is
left to whatever's in front of Denizen (see "Running with Docker" in the
README) — deliberately not something the container does itself.

Two things worth calling out:

- **Document scanner**: `<input type="file" accept="image/*"
  capture="environment">` for the actual capture (native camera UI, one
  photo per tap — no `getUserMedia` + hand-rolled camera preview needed for
  that part), then each captured photo is normalized through a canvas
  (`lib/scan.ts` — applies EXIF orientation, caps the longest side at 2000px,
  re-encodes as JPEG) and assembled into a single multi-page PDF by a
  hand-rolled writer (`lib/pdf.ts`) before upload, mirroring Google Drive's
  own scan-to-PDF flow. The PDF writer is a deliberately small, bounded
  subset of the spec — one full-page JPEG per page via the `DCTDecode`
  filter (the JPEG bytes are embedded as-is, no re-compression at that
  layer), no fonts, no general compression — a genuinely small and
  self-contained thing worth writing directly rather than taking on a
  general-purpose PDF library dependency for it (see CONTRIBUTING.md on
  this project's dependency philosophy — the same reasoning that keeps
  `internal/idgen`'s UUIDs hand-rolled while still pulling in a real tus
  client for resumable uploads, a genuinely complex piece of protocol).
  No edge detection/perspective correction (yet) — flagged as a possible
  follow-up once real usage says it's worth the added complexity, not
  built speculatively.
- **Web Share Target API** so other apps' "Share" menu can hand files
  straight to Denizen. Android/Chrome only for now (iOS Safari doesn't
  implement it for PWAs) — acceptable since Android is the only mobile
  target for this MVP. The action route (`/share-target`, declared in the
  manifest) receives a POST with the shared file(s) as `multipart/form-data`
  — but that POST comes from the OS, not this app's own authenticated
  `fetch()` wrapper (`lib/api.ts`), so there's no way to attach the
  `Authorization` header the real upload endpoint needs, and this app
  doesn't use cookie-based auth that a plain POST would carry automatically
  either. The service worker's `fetch` handler intercepts that POST before
  it ever reaches the network, stashes each file in the Cache Storage API
  (private per-origin storage, unrelated to this app's own file storage),
  and redirects (303) to `/share-target?share_id=...` — a normal SPA route
  (`routes/share-target/+page.svelte`) that reads the file(s) back out of
  the cache and hands them to the exact same authenticated `startUpload()`
  every other upload path already uses. No backend route or change was
  needed for any of this — it's resolved entirely client-side before the
  request would otherwise hit Go's router at all.
