package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"omni-folio/services/core/internal/application/fault"
)

const twrCheckpointResponseLimit = 3 * maxBodyBytes

func (s *Service) handlePortfolioTWRCheckpoints(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "TWR checkpoints do not accept query parameters"}})
		return
	}
	report, appErr := s.portfolio.TWRCheckpoints(r.Context())
	if appErr != nil {
		writeError(w, portfolioError(appErr))
		return
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded)+1 > twrCheckpointResponseLimit {
		writeError(w, fault.Internal(errors.New("TWR checkpoint response exceeds its declared bound")))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}
