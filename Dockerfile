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
# poppler-utils: pdftotext (real text layer) + pdftoppm (rasterizing a
# scanned page for OCR) — both used by internal/textextract. tesseract-ocr
# + its Italian/English word lists: the actual OCR pass for a scanned PDF
# (see ItemService.RunOCRSweep), this app's documents being mostly Italian
# with the occasional English one. All of these degrade gracefully if
# missing (PDFs — scanned or not — just aren't searchable), never a
# startup requirement.
RUN apk add --no-cache ca-certificates tzdata poppler-utils tesseract-ocr tesseract-ocr-data-ita tesseract-ocr-data-eng
COPY --from=backend /out/denizen /usr/local/bin/denizen
# DENIZEN_DATA_DIR (default ./data — see internal/config/config.go) should
# be bind- or volume-mounted here in any real deployment; the container
# itself doesn't assume a particular mount point.
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/denizen"]
