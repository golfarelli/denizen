# Denizen

**Denizen** is a self-hosted, open-source alternative to Google Drive / Nextcloud —
your own personal cloud storage, running on your own hardware.

> 🚧 Early development. Not usable yet — architecture and scaffolding stage.

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

## License

[AGPL-3.0](./LICENSE) — if you run a modified version of Denizen as a network
service, you must make your modifications available too.
