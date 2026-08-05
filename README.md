# Denizen

**Denizen** is a self-hosted, open-source alternative to Google Drive / Nextcloud —
your own personal cloud storage, running on your own hardware.

> 🚧 Early development. The whole MVP feature set — auth, folders, resumable
> uploads with progress and drag-and-drop, download, sharing, quotas, and
> admin user/invite management — works end to end through the UI now, with
> both a Go backend test suite and a Playwright end-to-end suite driving a
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
- An Android PWA client with a built-in document scanner and share-sheet
  integration.

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

## License

[AGPL-3.0](./LICENSE) — if you run a modified version of Denizen as a network
service, you must make your modifications available too.
