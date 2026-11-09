package fault

import (
	"errors"
	"log"
	"omni-folio/services/core/internal/modules/ledger/contracts"
)

type Error = contracts.Error

var ErrNotFound = contracts.ErrNotFound

func Internal(err error) *Error {
	log.Printf("internal error: %v", err)
	return &Error{Kind: contracts.InternalFailure, Body: contracts.APIError{Code: "internal_error", Message: "internal server error"}}
}
func As(err error) *Error {
	var result *Error
	if errors.As(err, &result) {
		return result
	}
	return Internal(err)
}
