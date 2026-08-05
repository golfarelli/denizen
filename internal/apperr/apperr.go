// Package apperr defines the uniform error shape every handler returns —
// {"error": {"code": "...", "message": "..."}} — per docs/ARCHITECTURE.md.
// Services return these directly so handlers can map them to an HTTP status
// without needing to know the specifics of what went wrong.
package apperr

import "net/http"

// Error is an application error carrying the HTTP status it should map to.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Validation(message string) *Error {
	return New(http.StatusBadRequest, "validation_error", message)
}

func Conflict(message string) *Error {
	return New(http.StatusConflict, "conflict", message)
}

var (
	Unauthorized  = New(http.StatusUnauthorized, "unauthorized", "invalid credentials")
	Forbidden     = New(http.StatusForbidden, "forbidden", "not allowed")
	NotFound      = New(http.StatusNotFound, "not_found", "resource not found")
	Internal      = New(http.StatusInternalServerError, "internal_error", "something went wrong")
	QuotaExceeded = New(http.StatusRequestEntityTooLarge, "quota_exceeded", "not enough storage quota remaining")
	DiskFull      = New(http.StatusInsufficientStorage, "disk_full", "server is out of disk space")
)
