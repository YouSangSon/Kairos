package contracts

import "errors"

type ErrorKind string

const (
	NotFound        ErrorKind = "not_found"
	InvalidInput    ErrorKind = "invalid_input"
	Conflict        ErrorKind = "conflict"
	InternalFailure ErrorKind = "internal"
)

type Error struct {
	Kind ErrorKind
	Body APIError
}

func (e *Error) Error() string { return e.Body.Message }

var ErrNotFound = errors.New("record not found")
