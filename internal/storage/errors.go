package storage

import "errors"

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")

	// ErrVersionMismatch is returned when the database version does not
	// match the current tool version. The user should run 'ssearch index'.
	ErrVersionMismatch = errors.New("database version mismatch")
)
