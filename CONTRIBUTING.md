# Contributing to Denizen

Thanks for your interest in Denizen. This document captures the conventions the
project follows, so contributions stay consistent no matter who wrote them.

## Git workflow (Gitflow)

- `master` — production-ready code only. Every commit on `master` is tagged with a version.
- `develop` — integration branch, base for new work.
- `feature/*` — new features, branched from and merged back into `develop`.
- `release/*` — release stabilization, branched from `develop`, merged into both `master` and `develop`.
- `hotfix/*` — urgent fixes on top of a release, branched from `master`, merged into both `master` and `develop`.

Merging into `master` and tagging a release is a public, hard-to-reverse action —
it always requires explicit sign-off from the project owner. Regular work on
`feature/*`/`develop` doesn't.

## Versioning

[Semantic Versioning](https://semver.org/): `major.minor.patch`. Every release
merged into `master` is tagged `vX.Y.Z` (e.g. `v1.12.3`).

- **patch** — bug fixes only, no new features, no breaking changes.
- **minor** — new, backward-compatible features.
- **major** — breaking changes.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `type: short description`,
using `feat`, `fix`, `refactor`, `test`, `docs`, `chore`. Keeps history greppable
and maps naturally onto the semver bump a change implies.

## Minimal dependencies

Prefer implementing something ourselves over adding a dependency when the logic
involved is small — it keeps the project resilient to unmaintained or broken
upstream packages. Exceptions go to genuinely complex infrastructure that would
be substantial and risky to reimplement (e.g. the tus resumable-upload protocol,
the SQLite driver, the JWT library, PDF/Word/Excel *rendering* via pdf.js,
docx-preview, and xlsx) — those stay as dependencies. The line isn't always
obvious from the file format alone: this
project's own PDF *writer* (`lib/pdf.ts`, used by the camera scanner) is hand-
rolled, because emitting one full-page JPEG per page is a small, bounded
problem — parsing and rendering an arbitrary PDF someone else produced is not.

## Naming conventions

Consistency beats personal preference. Once a name is established for a concept,
reuse it everywhere rather than inventing a synonym:

| Name | Meaning |
|---|---|
| `item` / `items` | a query result row / rows |
| `cn` | a database connection |
| `sql` | a SQL query string/statement |
| `res` | an HTTP response object, or a function/method return value |

This glossary grows as new recurring concepts appear in the codebase — extend
it in this file when you introduce one, instead of leaving it implicit.

> **Go-specific gotcha:** a local variable named `sql` shadows the standard
> `database/sql` package within its scope, so `sql.ErrNoRows`/`sql.DB`/etc.
> become unreachable by name after the shadowing line. Repository files
> import it under an alias instead — `stdsql "database/sql"` — which keeps
> `sql` free for the query-string convention above. See
> `internal/repository/*.go` for the pattern.

## Request handling flow

Every handler follows the same order of operations, so any one of them reads
the same way top to bottom:

1. Validate the input.
2. Transform/normalize the input.
3. Perform the read or write (query).
4. Transform/shape the output.

## Backend architecture

Three layers, one responsibility each:

- **Handler** — HTTP only: parse the request, validate input, call the service,
  write the response. No SQL, no business logic.
- **Service** — business logic. No HTTP concerns, no raw SQL.
- **Repository** — SQL queries only. No business logic.

## Testing

- **Unit tests** — normal, focused, may use fakes/mocks where reasonable.
- **Flow tests** (Go, `test/flow/`) — end-to-end, *no mocks*: start a real
  server on a random port, issue real HTTP requests (including the full tus
  protocol for uploads), then assert on the real result — every relevant
  field of the DB record after the operation, and, where applicable, a
  byte-for-byte comparison of the file actually written to storage against
  the input.
- **E2E tests** (`web/e2e/`, Playwright) — the frontend's counterpart to Go's
  flow tests, same "no mocks" spirit: a real Chromium browser drives the
  actual built frontend (`npm run build`'s output, not `npm run dev`) served
  by the actual `denizen` binary, talking to a real (freshly created,
  disposable) SQLite database — not a single backend call or DOM interaction
  is faked. Run with `npm run test:e2e` from `web/`.
