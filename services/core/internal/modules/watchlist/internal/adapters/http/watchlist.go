package http

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	watchapi "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/modules/watchlist/internal/adapters/jsoninput"
	"omni-folio/services/core/internal/transport/httpjson"
)

func watchlistError(err error) *appError {
	switch {
	case errors.Is(err, watchapi.ErrInput):
		return &appError{Status: http.StatusBadRequest, Body: apiError{Code: "watchlist_invalid", Message: "watchlist input does not meet the declared contract"}}
	case errors.Is(err, watchapi.ErrChanged):
		return &appError{Status: http.StatusConflict, Body: apiError{Code: "watchlist_changed", Message: "watchlist changed; reload before editing"}}
	default:
		return &appError{Status: http.StatusServiceUnavailable, Body: apiError{Code: "watchlist_unavailable", Message: "watchlist could not be confirmed; retry the same change or reload"}}
	}
}

func (s *handler) handleWatchlist(w http.ResponseWriter, r *http.Request) {
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
		change, decodeErr := jsoninput.Decode(raw)
		if decodeErr != nil {
			writeError(w, watchlistError(decodeErr))
			return
		}
		result, err = s.api.Replace(r.Context(), *change)
	} else {
		result, err = s.api.Read(r.Context())
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

type handler struct{ api watchapi.API }
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type appError struct {
	Status int
	Body   apiError
}

func writeError(w http.ResponseWriter, err *appError) { httpjson.Write(w, err.Status, err.Body) }

func Register(mux *http.ServeMux, api watchapi.API) {
	h := &handler{api: api}
	mux.HandleFunc("GET /v1/watchlist", h.handleWatchlist)
	mux.HandleFunc("POST /v1/watchlist", h.handleWatchlist)
}
