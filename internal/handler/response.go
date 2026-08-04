// Package handler is the HTTP layer: parse the request, validate input,
// call a service, write the response. No SQL, no business logic — see
// internal/service and internal/repository.
package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/golfarelli/denizen/internal/apperr"
)

func writeJSON(res http.ResponseWriter, status int, body any) {
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(res).Encode(body); err != nil {
		log.Printf("handler: encode response: %v", err)
	}
}

type errorEnvelope struct {
	Error apperr.Error `json:"error"`
}

// writeError maps a service error to the uniform {"error": {code, message}}
// envelope. Errors that aren't an *apperr.Error are a bug somewhere below
// this layer, not something to describe to the client — they become a
// generic 500.
func writeError(res http.ResponseWriter, err error) {
	appErr, ok := err.(*apperr.Error)
	if !ok {
		log.Printf("handler: unhandled error: %v", err)
		appErr = apperr.Internal
	}
	writeJSON(res, appErr.Status, errorEnvelope{Error: *appErr})
}
