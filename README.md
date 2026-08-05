# Denizen

**Denizen** is a self-hosted, open-source alternative to Google Drive / Nextcloud —
your own personal cloud storage, running on your own hardware.

> 🚧 Early development. The backend (auth, folders, resumable uploads,
> sharing, quotas) and a first slice of the frontend (login/register, folder
> browsing, download, delete) work end to end — still missing an upload UI,
> a sharing UI, and more.

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

## License

[AGPL-3.0](./LICENSE) — if you run a modified version of Denizen as a network
service, you must make your modifications available too.
