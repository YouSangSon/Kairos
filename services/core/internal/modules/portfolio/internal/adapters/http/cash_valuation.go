package http

import (
	"net/http"
	"net/url"
)

func (s *handler) handleCashValuation(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "query parameters are invalid"}})
		return
	}
	for key := range query {
		if key != "base_currency" && key != "as_of" {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "unknown query parameter " + key, Field: key}})
			return
		}
	}
	for _, key := range []string{"base_currency", "as_of"} {
		if len(query[key]) != 1 || query[key][0] == "" {
			writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: key + " query parameter must appear exactly once", Field: key}})
			return
		}
	}
	valuation, appErr := s.api.CashValuation(r.Context(), query.Get("base_currency"), query.Get("as_of"))
	if appErr != nil {
		writeError(w, portfolioError(appErr))
		return
	}
	writeJSON(w, http.StatusOK, valuation)
}
