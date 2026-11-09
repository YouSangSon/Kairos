package main

import (
	"net/http"
)

func (s *Service) handleCurrentPortfolioValuation(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{
			Code: "invalid_query", Message: "current portfolio valuation does not accept query parameters",
		}})
		return
	}
	valuation, appErr := s.portfolio.CurrentValuation(r.Context())
	if appErr != nil {
		writeError(w, portfolioError(appErr))
		return
	}
	writeJSON(w, http.StatusOK, valuation)
}
