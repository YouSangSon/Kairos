package main

import (
	"errors"
	"net/http"
	"omni-folio/services/core/internal/application/fault"
	p "omni-folio/services/core/internal/modules/portfolio/contracts"
)

func portfolioError(err error) *appError {
	var f *p.Failure
	if errors.As(err, &f) {
		status := http.StatusBadRequest
		if f.Conflict {
			status = http.StatusConflict
		}
		return &appError{Status: status, Body: APIError{Code: f.Code, Message: f.Message, Field: f.Field}}
	}
	var existing *appError
	if errors.As(err, &existing) {
		return existing
	}
	return fault.Internal(err)
}
