package http

import (
	"net/http"
)

func (s *handler) handlePortfolioBenchmarks(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "portfolio benchmark comparison does not accept query parameters"}})
		return
	}
	report, appErr := s.api.Benchmarks(r.Context())
	if appErr != nil {
		writeError(w, portfolioError(appErr))
		return
	}
	writeJSON(w, http.StatusOK, report)
}
