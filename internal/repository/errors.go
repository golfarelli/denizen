// Package repository is the only place SQL is allowed to appear. Every
// function here does exactly one query — no business logic (see
// internal/service for that).
package repository

import "errors"

// ErrNotFound is returned by Get* methods when no row matches.
var ErrNotFound = errors.New("repository: not found")
