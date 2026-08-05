# syntax=docker/dockerfile:1

# --- frontend: build the SvelteKit SPA -------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build
# Output lands in /src/internal/webui/dist (see web/vite.config.ts's
# adapter-static "pages"/"assets" config) — deliberately outside this
# stage's own web/ directory, one level up, so it's ready to hand to the
# backend stage below at the exact path go:embed expects it at.

# --- backend: build the Go binary, embedding the frontend above -------------
FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -o /out/denizen ./cmd/server

# --- runtime: just the binary -------------------------------------------------
FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /out/denizen /usr/local/bin/denizen
# DENIZEN_DATA_DIR (default ./data — see internal/config/config.go) should
# be bind- or volume-mounted here in any real deployment; the container
# itself doesn't assume a particular mount point.
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/denizen"]
