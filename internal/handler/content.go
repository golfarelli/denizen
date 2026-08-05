package handler

import (
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/golfarelli/denizen/internal/apperr"
	"github.com/golfarelli/denizen/internal/httpio"
	"github.com/golfarelli/denizen/internal/model"
)

// serveFileContent streams item's bytes from path, with Range support —
// shared by ItemHandler.Content (private, authenticated) and
// ShareHandler.PublicContent (public, via a share token), since both boil
// down to "serve this already-authorized file's bytes" once the caller has
// done its own authorization.
func serveFileContent(res http.ResponseWriter, req *http.Request, item *model.Item, path string) {
	f, err := os.Open(path)
	if err != nil {
		httpio.WriteError(res, apperr.Internal)
		return
	}
	defer f.Close()

	if item.MimeType != nil {
		res.Header().Set("Content-Type", *item.MimeType)
	}
	// RFC 2231/6266 encoding via mime.FormatMediaType handles filenames with
	// quotes, unicode, etc. correctly — safer than hand-quoting the name.
	res.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name}))

	http.ServeContent(res, req, item.Name, time.Unix(item.UpdatedAt, 0), f)
}
