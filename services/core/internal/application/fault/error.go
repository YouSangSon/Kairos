package fault

import (
	"errors"
	"log"
	"omni-folio/services/core/internal/ledgerdomain"
)

var ErrNotFound = errors.New("record not found")

type Error struct {
	Status int
	Body   ledgerdomain.APIError
}

func (e *Error) Error() string { return e.Body.Message }

func Internal(err error) *Error {
	log.Printf("internal error: %v", err)
	return &Error{500, ledgerdomain.APIError{Code: "internal_error", Message: "internal server error"}}
}

func As(err error) *Error {
	var result *Error
	if errors.As(err, &result) {
		return result
	}
	return Internal(err)
}
