package http

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"

	"omni-folio/services/core/internal/modules/research/contracts"
	"omni-folio/services/core/internal/transport/httpjson"
)

var researchSymbol = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,31}$`)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Register(mux *http.ServeMux, queries contracts.Queries) {
	mux.HandleFunc("GET /v1/research/feed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query) != 2 || len(query["market"]) != 1 || len(query["symbol"]) != 1 ||
			(query.Get("market") != "KR" && query.Get("market") != "US") || !researchSymbol.MatchString(query.Get("symbol")) {
			httpjson.Write(w, 400, apiError{Code: "invalid_query", Message: "choose one KR or US market and canonical symbol"})
			return
		}

		view, err := queries.PublicResearch(query.Get("market"), query.Get("symbol"))
		if err != nil {
			status, code, message := 503, "research_feed_unavailable", "public research file is unavailable"
			switch {
			case errors.Is(err, contracts.ErrNotConfigured):
				code, message = "research_not_configured", "public research is not configured"
			case errors.Is(err, contracts.ErrFeedMissing):
				status, code, message = 404, "research_feed_missing", "public research file is missing"
			}
			httpjson.Write(w, status, apiError{Code: code, Message: message})
			return
		}
		httpjson.Write(w, 200, view)
	})
}
