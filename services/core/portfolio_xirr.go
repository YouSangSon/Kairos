package main

import (
	"net/http"
)

func (s *Service) handlePortfolioXIRR(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "portfolio XIRR does not accept query parameters"}})
		return
	}
	report, appErr := s.portfolio.XIRR(r.Context())
	if appErr != nil {
		writeError(w, portfolioError(appErr))
		return
	}
	writeJSON(w, http.StatusOK, report)
}
