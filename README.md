# Denizen

**Denizen** is a self-hosted, open-source alternative to Google Drive / Nextcloud —
your own personal cloud storage, running on your own hardware.

> 🚧 Early development. The whole MVP feature set — auth, folders, resumable
> uploads with progress and drag-and-drop, download, rename, move, copy,
> trash (with restore), sharing, quotas, and admin user/invite management —
> works end to end through the UI now, including a mobile-friendly kebab
> action menu on each row and an inline preview (images, PDFs, videos,
> Word/Excel documents, text) when you tap a file instead of only ever
> downloading it. The app is also an installable PWA now: a real
> manifest + service worker, receiving files shared from other Android apps
> via the OS share sheet, and a camera-based document scanner that saves
> captures as a multi-page PDF, same as Google Drive's own scan flow.
> Installability and the share target both need HTTPS (or `localhost`) to
> work at all — see "Running with Docker" below. Word/Excel/PowerPoint
> documents can also open with genuine, high-fidelity rendering (view-only
> for now) via an optional OnlyOffice Document Server integration — a real
> second container, entirely opt-in, see `docker-compose.onlyoffice.yml`.
> Both a Go backend test suite and a Playwright end-to-end suite drive a
> real browser against the real binary.

## Why

Most people don't want to trust their files to a corporation, but self-hosting
a real Drive replacement today usually means either a heavyweight platform or
piecing several projects together. Denizen aims to be a single, small,
opinionated, container-native alternative that runs comfortably on modest
home-server hardware.

## Goals for the first release (MVP)

- File storage: upload, download, folders, share links — no online office
  editing, no desktop sync client (yet).
- Multi-user, invite-only (no public registration).
- A single container: Go backend + embedded SvelteKit frontend.
- The same app installable as an Android PWA, with a built-in document
  scanner and share-sheet integration — not a separate client, the same
  responsive web app extended with a manifest, a service worker, and a
  scan-to-PDF flow (see "Progressive Web App" in `docs/ARCHITECTURE.md`).

## Tech stack

| | |
|---|---|
| Backend | Go |
| Frontend | SvelteKit |
| Database | SQLite (WAL mode) + [Litestream](https://litestream.io/) for continuous backup |
| File storage | Local filesystem, mirroring the user's own folder structure |
| Uploads | Resumable, chunked ([tus protocol](https://tus.io/)) |
| Auth | JWT (access + refresh tokens) |
| Deployment | Single Docker container |

See [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) for the full design
write-up, and [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the conventions this
codebase follows (git workflow, versioning, naming, testing).

## Building & running locally

The frontend has to be built *before* the Go binary, since it's embedded
into it (`internal/webui`, via `go:embed`) rather than served separately:

```sh
cd web && npm install && npm run build && cd ..
go build -o denizen ./cmd/server
./denizen
```

On first run, with no accounts yet, the server logs an invite code — use it
to create the admin account via the UI (or `POST /api/v1/auth/register`).
See `internal/config/config.go` for the environment variables that control
where data lives, token lifetimes, quotas, and background sweep intervals.

For frontend-only iteration, `cd web && npm run dev` runs SvelteKit's own
dev server (proxying `/api` and `/s` to a Go instance — see
`web/vite.config.ts`); `go test ./...` from the repo root runs the backend's
own test suite and doesn't need the frontend built at all.

### Running the tests

```sh
go test ./...              # backend: unit + flow tests (no mocks — see CONTRIBUTING.md)
cd web && npm run test:e2e # frontend: a real browser against the real binary (first run: npx playwright install chromium)
```

## Running with Docker

```sh
git clone https://github.com/golfarelli/denizen.git && cd denizen
docker compose up -d --build
```

`Dockerfile` is a multi-stage build (frontend → backend → a minimal runtime
image with nothing but the resulting binary) that needs no local Go or
Node.js install — everything happens inside the build. See
`docker-compose.yml` for the environment variables to set (in particular,
replace `DENIZEN_JWT_SECRET`'s placeholder with a real random value —
`openssl rand -hex 32` — before running this anywhere but a throwaway local
test) and where data persists on the host.

Denizen itself only ever speaks plain HTTP — TLS termination is deliberately
left to whatever sits in front of it (a reverse proxy, a tunnel, your
router), not baked into the container. That's a hard requirement, not just
best practice, for the PWA features specifically: service workers (so
installability and the share target — see below) refuse to register outside
a secure context (HTTPS or `localhost`), so reaching Denizen over plain HTTP
via a LAN IP, as in a minimal home-server setup, means those two features
are silently unavailable — the rest of the app is unaffected either way.

### Optional: high-fidelity Word/Excel/PowerPoint viewing

```sh
docker compose -f docker-compose.yml -f docker-compose.onlyoffice.yml up -d --build
```

Adds a real [OnlyOffice Document Server](https://github.com/ONLYOFFICE/DocumentServer)
— genuinely heavy (its own container, its own real Office-compatible
rendering engine), so it's an entirely separate, opt-in overlay rather than
part of the default single-container setup. Without it, Word/Excel still
preview client-side (docx-preview/xlsx) with lower fidelity and no
PowerPoint support at all. See `docker-compose.onlyoffice.yml`'s own
comments for the environment variables both sides need to agree on. Viewing
only for now — editing is a planned follow-up, see `docs/ARCHITECTURE.md`.

## License

[AGPL-3.0](./LICENSE) — if you run a modified version of Denizen as a network
service, you must make your modifications available too.
