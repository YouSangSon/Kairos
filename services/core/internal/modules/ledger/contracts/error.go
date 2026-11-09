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
	Kind  ErrorKind
	Body  APIError
	cause error
}

func (e *Error) Error() string { return e.Body.Message }

var ErrNotFound = errors.New("record not found")

func (e *Error) Unwrap() error { return e.cause }
func InternalError(cause error) *Error {
	return &Error{Kind: InternalFailure, Body: APIError{Code: "internal_error", Message: "internal server error"}, cause: cause}
}
func AsError(err error) *Error {
	var result *Error
	if errors.As(err, &result) {
		return result
	}
	return InternalError(err)
}
