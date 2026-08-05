// Package upload wires up resumable (tus protocol) file uploads. It's a
// thin adapter: the actual tus protocol implementation is the tusd library
// (embedded here, not run as a separate process — see docs/ARCHITECTURE.md);
// this package's own job is authentication, ownership checks, and calling
// internal/service to turn a finished upload into a real item.
//
// Every request to this handler — not just the ones this package's own
// hooks run on — is expected to already have passed through
// internal/middleware.RequireAuth (see internal/router), because tusd only
// exposes hooks for create/finish/terminate, not for the PATCH/HEAD/GET
// requests that continue or read back an in-progress upload. Wrapping the
// whole handler in RequireAuth is what closes that gap; the hooks below
// then read the claims that middleware already put in the request context
// (confirmed to survive into HookEvent.Context — see tusd's httpContext,
// which layers context.WithoutCancel over the original request context
// specifically to preserve values like this).
package upload

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/middleware"
	"github.com/golfarelli/denizen/internal/service"
)

// BasePath is where the tus endpoints are mounted — see internal/router.
const BasePath = "/api/v1/uploads/"

// Metadata keys the client is expected to set via the tus Upload-Metadata
// header. "filename" is a de facto standard among tus clients; "parent_id"
// and "filetype" are Denizen-specific (filetype falls back to a guess from
// the filename's extension if omitted).
const (
	metaFilename = "filename"
	metaParentID = "parent_id"
	metaFileType = "filetype"
	metaOwnerID  = "owner_id" // server-set only, see preCreate — never trust a client-supplied value
)

// NewHandler builds the tus HTTP handler, backed by a local filestore
// rooted at stagingDir (must be on the same filesystem as every user's
// files root — finalizing an upload is a Rename, not a copy).
func NewHandler(stagingDir string, items *service.ItemService, maxUploadSizeBytes int64) (http.Handler, error) {
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, err
	}

	store := filestore.New(stagingDir)
	locker := memorylocker.New()
	composer := handler.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	h := &hooks{items: items}

	return handler.NewHandler(handler.Config{
		StoreComposer:              composer,
		BasePath:                   BasePath,
		MaxSize:                    maxUploadSizeBytes,
		PreUploadCreateCallback:    h.preCreate,
		PreFinishResponseCallback:  h.preFinish,
		PreUploadTerminateCallback: h.preTerminate,
		// Without this, the absolute Location URL tusd returns on upload
		// creation (which tus-js-client then uses for every subsequent
		// PATCH/HEAD) is built from the raw scheme/host of the connection
		// tusd itself receives — correct for the plain-HTTP LAN deployment,
		// but wrong the moment anything sits in front as a TLS-terminating
		// reverse proxy (Tailscale's `tailscale serve`, or any future one):
		// tusd would still see plain HTTP from the proxy and hand back an
		// http:// URL to a client that loaded the page over https://,
		// breaking every upload past the initial POST. Trusting
		// X-Forwarded-Proto/-Host instead is what makes that URL match what
		// the client actually connected over.
		RespectForwardedHeaders: true,
	})
}

type hooks struct {
	items *service.ItemService
}

// preCreate runs on the initial POST that starts an upload. It authenticates
// the caller, validates the declared destination folder up front (so a bad
// parent_id fails immediately instead of after megabytes of upload), and
// stamps the upload's metadata with the server-verified owner — every
// later hook trusts *that* copy, not whatever the client claims.
func (h *hooks) preCreate(event handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	claims, ok := middleware.ClaimsFromContext(event.Context)
	if !ok {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, unauthorizedTusError()
	}

	filename := strings.TrimSpace(event.Upload.MetaData[metaFilename])
	if filename == "" {
		return handler.HTTPResponse{}, handler.FileInfoChanges{},
			handler.NewError("ERR_INVALID_METADATA", metaFilename+" metadata is required", http.StatusBadRequest)
	}

	parentID := parentIDFromMetadata(event.Upload.MetaData)
	if err := h.items.ValidateFolder(event.Context, claims.UserID, parentID); err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, toTusError(err)
	}

	// Optimistic check against the client's declared Upload-Length — fails
	// fast, before a single byte is staged, instead of only discovering the
	// quota is blown after however long the upload takes. The definitive
	// check happens again in FinalizeUpload, since two uploads racing each
	// other could both pass this one.
	if !event.Upload.SizeIsDeferred {
		if err := h.items.CheckQuota(event.Context, claims.UserID, event.Upload.Size); err != nil {
			return handler.HTTPResponse{}, handler.FileInfoChanges{}, toTusError(err)
		}
	}

	meta := make(handler.MetaData, len(event.Upload.MetaData)+1)
	for k, v := range event.Upload.MetaData {
		meta[k] = v
	}
	meta[metaOwnerID] = claims.UserID

	return handler.HTTPResponse{}, handler.FileInfoChanges{MetaData: meta}, nil
}

// preFinish runs once an upload's bytes are fully received, before the
// completing request gets its response — so by the time the client sees
// success, the item already exists (no race between "upload done" and
// "file visible in the tree").
func (h *hooks) preFinish(event handler.HookEvent) (handler.HTTPResponse, error) {
	claims, ok := middleware.ClaimsFromContext(event.Context)
	if !ok {
		return handler.HTTPResponse{}, unauthorizedTusError()
	}
	if event.Upload.MetaData[metaOwnerID] != claims.UserID {
		return handler.HTTPResponse{}, forbiddenTusError()
	}

	filename := event.Upload.MetaData[metaFilename]
	parentID := parentIDFromMetadata(event.Upload.MetaData)

	mimeType := event.Upload.MetaData[metaFileType]
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(filename))
	}

	sourcePath := event.Upload.Storage[filestore.StorageKeyPath]
	if sourcePath == "" {
		return handler.HTTPResponse{}, handler.NewError("ERR_INTERNAL", "upload storage path missing", http.StatusInternalServerError)
	}

	item, err := h.items.FinalizeUpload(event.Context, claims.UserID, parentID, filename, sourcePath, event.Upload.Size, mimeType)
	if err != nil {
		return handler.HTTPResponse{}, toTusError(err)
	}

	return handler.HTTPResponse{Header: handler.HTTPHeader{"X-Item-Id": item.ID}}, nil
}

// preTerminate runs on a DELETE that cancels an in-progress upload —
// without this check, anyone who obtained (or guessed) an upload's URL
// could cancel someone else's upload.
func (h *hooks) preTerminate(event handler.HookEvent) (handler.HTTPResponse, error) {
	claims, ok := middleware.ClaimsFromContext(event.Context)
	if !ok {
		return handler.HTTPResponse{}, unauthorizedTusError()
	}
	if event.Upload.MetaData[metaOwnerID] != claims.UserID {
		return handler.HTTPResponse{}, forbiddenTusError()
	}
	return handler.HTTPResponse{}, nil
}

func parentIDFromMetadata(meta handler.MetaData) *string {
	v := strings.TrimSpace(meta[metaParentID])
	if v == "" {
		return nil
	}
	return &v
}

func unauthorizedTusError() error {
	return handler.NewError("ERR_UNAUTHORIZED", "unauthorized", http.StatusUnauthorized)
}

func forbiddenTusError() error {
	return handler.NewError("ERR_FORBIDDEN", "not allowed", http.StatusForbidden)
}

// toTusError maps an apperr.Error from internal/service to tusd's own error
// type — tus endpoints follow the tus protocol's error conventions, not
// this codebase's regular {"error": {code, message}} JSON envelope, since
// they're not regular JSON REST endpoints.
func toTusError(err error) error {
	if appErr, ok := err.(*apperr.Error); ok {
		return handler.NewError("ERR_"+strings.ToUpper(appErr.Code), appErr.Message, appErr.Status)
	}
	return handler.NewError("ERR_INTERNAL", "something went wrong", http.StatusInternalServerError)
}
