package composition

import (
	"fmt"
	"net/http"
	"net/url"
	"omni-folio/services/core/internal/transport/httpjson"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func ValidateAllowedOrigin(origin string) error {
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("allow-origin must be an exact http(s) origin without path, query, credentials, or fragment")
	}
	return nil
}

func WithCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.URL.Path == "/v1/market-data/imports/preview" || r.URL.Path == "/v1/market-data/imports/apply") || r.URL.Path == "/v1/watchlist" {
			// CORS headers alone do not prevent writes from an unapproved browser origin.
			origins, present := r.Header[http.CanonicalHeaderKey("Origin")]
			if present && (len(origins) != 1 || origins[0] == "" || allowedOrigin == "" || origins[0] != allowedOrigin) {
				if r.URL.Path == "/v1/watchlist" {
					httpjson.Write(w, http.StatusForbidden, APIError{Code: "watchlist_origin_rejected", Message: "watchlist origin is not allowed"})
				} else {
					httpjson.Write(w, http.StatusForbidden, APIError{Code: "owner_data_origin_rejected", Message: "owner data origin is not allowed"})
				}
				return
			}
		}
		if allowedOrigin == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		if r.Header.Get("Origin") != allowedOrigin {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		if r.Method == http.MethodOptions {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
