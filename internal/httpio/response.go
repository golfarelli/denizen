// Package httpio writes the uniform JSON response/error envelope described
// in docs/ARCHITECTURE.md. Split out from internal/handler so that
// internal/middleware — which also needs to write an error response, e.g.
// on a missing/invalid bearer token — can depend on it without handler and
// middleware ending up importing each other.
package httpio

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
)

// WriteJSON writes body as a JSON response with the given status.
func WriteJSON(res http.ResponseWriter, status int, body any) {
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(res).Encode(body); err != nil {
		log.Printf("httpio: encode response: %v", err)
	}
}

type errorEnvelope struct {
	Error apperr.Error `json:"error"`
}

// WriteError maps a service error to the uniform {"error": {code, message}}
// envelope. Errors that aren't an *apperr.Error are a bug somewhere below
// this layer, not something to describe to the client — they become a
// generic 500.
func WriteError(res http.ResponseWriter, err error) {
	appErr, ok := err.(*apperr.Error)
	if !ok {
		log.Printf("httpio: unhandled error: %v", err)
		appErr = apperr.Internal
	}
	WriteJSON(res, appErr.Status, errorEnvelope{Error: *appErr})
}
