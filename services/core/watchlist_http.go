package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	watchlistmodule "omni-folio/services/core/internal/modules/watchlist"
	watchapi "omni-folio/services/core/internal/modules/watchlist/contracts"
)

func watchlistError(err error) *appError {
	switch {
	case errors.Is(err, watchapi.ErrInput):
		return &appError{Status: http.StatusBadRequest, Body: APIError{Code: "watchlist_invalid", Message: "watchlist input does not meet the declared contract"}}
	case errors.Is(err, watchapi.ErrChanged):
		return &appError{Status: http.StatusConflict, Body: APIError{Code: "watchlist_changed", Message: "watchlist changed; reload before editing"}}
	default:
		return &appError{Status: http.StatusServiceUnavailable, Body: APIError{Code: "watchlist_unavailable", Message: "watchlist could not be confirmed; retry the same change or reload"}}
	}
}

func (s *Service) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeError(w, watchlistError(watchapi.ErrInput))
		return
	}
	var result any
	var err error
	if r.Method == http.MethodPost {
		mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaErr != nil || mediaType != "application/json" {
			writeError(w, watchlistError(watchapi.ErrInput))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, watchapi.BodyLimit)
		defer r.Body.Close()
		raw, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writeError(w, watchlistError(watchapi.ErrInput))
			return
		}
		change, decodeErr := watchlistmodule.DecodeChange(raw)
		if decodeErr != nil {
			writeError(w, watchlistError(decodeErr))
			return
		}
		result, err = s.watchlist.Replace(r.Context(), *change)
	} else {
		result, err = s.watchlist.Read(r.Context())
	}
	if err != nil {
		writeError(w, watchlistError(err))
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded)+1 > watchapi.ResponseLimit {
		writeError(w, watchlistError(errors.New("watchlist response exceeds its bound")))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}
