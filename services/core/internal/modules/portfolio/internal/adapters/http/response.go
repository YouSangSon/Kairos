package http

import (
	"errors"
	"net/http"
	"omni-folio/services/core/internal/application/fault"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"omni-folio/services/core/internal/transport/httpjson"
)

type APIError = c.APIError
type appError struct {
	Status int
	Body   APIError
}

func portfolioError(err error) *appError {
	var f *c.Failure
	if errors.As(err, &f) {
		status := http.StatusBadRequest
		if f.Conflict {
			status = http.StatusConflict
		}
		return &appError{Status: status, Body: APIError{Code: f.Code, Message: f.Message, Field: f.Field}}
	}
	existing := fault.As(err)
	return &appError{Status: existing.Status, Body: APIError{Code: existing.Body.Code, Message: existing.Body.Message, Field: existing.Body.Field}}
}

func writeJSON(w http.ResponseWriter, status int, body any) { httpjson.Write(w, status, body) }
func writeError(w http.ResponseWriter, err *appError)       { httpjson.Write(w, err.Status, err.Body) }
