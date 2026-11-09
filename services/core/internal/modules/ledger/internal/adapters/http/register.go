package httpadapter

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	cursorcodec "omni-folio/services/core/internal/modules/ledger/internal/adapters/cursor"
	"omni-folio/services/core/internal/transport/httpjson"
	"regexp"
	"strconv"
	"unicode/utf8"
)

type APIError = c.APIError
type ApplyRequest = c.ApplyRequest
type ledgerActivityCursor = c.ActivityCursor

const maxBodyBytes = 1 << 20
const defaultActivityLimit = 50

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

type appError struct {
	Status int
	Body   c.APIError
}
type handler struct {
	api c.Service
	key [32]byte
}

func Register(mux *http.ServeMux, api c.Service, key [32]byte) {
	s := &handler{api: api, key: key}
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("POST /v1/imports/preview", s.handlePreview)
	mux.HandleFunc("POST /v1/imports/apply", s.handleApply)
	mux.HandleFunc("GET /v1/portfolio/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /v1/portfolio/instrument-ledger", s.handleInstrumentLedger)
	mux.HandleFunc("GET /v1/ledger/activities", s.handleLedgerActivities)
	mux.HandleFunc("GET /v1/broker-reconciliation/latest", s.handleReconciliation)
}
func writeJSON(w http.ResponseWriter, status int, body any) { httpjson.Write(w, status, body) }
func writeError(w http.ResponseWriter, e *appError)         { writeJSON(w, e.Status, e.Body) }
func fromContract(e *c.Error) *appError {
	status := http.StatusInternalServerError
	switch e.Kind {
	case c.InvalidInput:
		status = http.StatusBadRequest
	case c.NotFound:
		status = http.StatusNotFound
	case c.Conflict:
		status = http.StatusConflict
	}
	return &appError{status, e.Body}
}
func (s *handler) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	value, err := s.api.Snapshot(r.Context())
	if err != nil {
		writeError(w, fromContract(c.InternalError(err)))
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *handler) handleReconciliation(w http.ResponseWriter, r *http.Request) {
	value, err := s.api.Reconciliation(r.Context())
	if errors.Is(err, c.ErrNotFound) {
		writeError(w, &appError{http.StatusNotFound, c.APIError{Code: "broker_reconciliation_not_found", Message: "broker reconciliation was not found"}})
		return
	}
	if err != nil {
		writeError(w, fromContract(c.InternalError(err)))
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	value, err := s.api.Status(r.Context())
	if err != nil {
		writeError(w, fromContract(c.InternalError(err)))
		return
	}
	trustState := "never_verified"
	if value.LastVerifiedAt != nil {
		trustState = "verified"
	}
	writeJSON(w, http.StatusOK, struct {
		Service        string       `json:"service"`
		LiveEnabled    bool         `json:"live_enabled"`
		Mode           string       `json:"mode"`
		TrustState     string       `json:"trust_state"`
		LedgerRevision string       `json:"ledger_revision"`
		LastVerifiedAt *string      `json:"last_verified_at"`
		Issues         []c.APIError `json:"issues"`
	}{"omni-folio", false, "local_import_only", trustState, value.LedgerRevision, value.LastVerifiedAt, []c.APIError{}})
}
func (s *handler) handlePreview(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/csv" {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_content_type", Message: "Content-Type must be text/csv"}})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_body", Message: "CSV body is too large or unreadable"}})
		return
	}
	preview, appErr := s.api.PreviewImport(r.Context(), body)
	if appErr != nil {
		writeError(w, fromContract(appErr))
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *handler) handleApply(w http.ResponseWriter, r *http.Request) {
	var req ApplyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	receipt, appErr := s.api.ApplyImport(r.Context(), req)
	if appErr != nil {
		writeError(w, fromContract(appErr))
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *handler) handleInstrumentLedger(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 2 || len(query["instrument_id"]) != 1 || len(query["currency"]) != 1 ||
		query.Get("instrument_id") == "" || !utf8.ValidString(query.Get("instrument_id")) || !currencyCodePattern.MatchString(query.Get("currency")) {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "instrument ledger query is invalid"}})
		return
	}
	result, appErr := s.api.InstrumentActivity(r.Context(), query.Get("instrument_id"), query.Get("currency"))
	if appErr != nil {
		writeError(w, fromContract(appErr))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *handler) handleLedgerActivities(w http.ResponseWriter, r *http.Request) {
	limit, cursor, appErr := s.parseLedgerActivityQuery(r)
	if appErr != nil {
		writeError(w, appErr)
		return
	}
	result, err := s.api.Activity(r.Context(), limit, cursor)
	if err != nil {
		if errors.Is(err, c.ErrInvalidActivityCursor) {
			writeError(w, invalidActivityQuery())
			return
		}
		writeError(w, fromContract(c.InternalError(err)))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *handler) parseLedgerActivityQuery(r *http.Request) (int, *ledgerActivityCursor, *appError) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, nil, invalidActivityQuery()
	}
	for key := range query {
		if key != "limit" && key != "cursor" {
			return 0, nil, invalidActivityQuery()
		}
	}
	limit := defaultActivityLimit
	if values, ok := query["limit"]; ok {
		if len(values) != 1 {
			return 0, nil, invalidActivityQuery()
		}
		parsed, err := strconv.Atoi(values[0])
		if err != nil || parsed < 1 || parsed > 100 || strconv.Itoa(parsed) != values[0] {
			return 0, nil, invalidActivityQuery()
		}
		limit = parsed
	}
	var cursor *ledgerActivityCursor
	if values, ok := query["cursor"]; ok {
		if len(values) != 1 {
			return 0, nil, invalidActivityQuery()
		}
		decoded, err := cursorcodec.Decode(s.key, values[0])
		if err != nil {
			return 0, nil, invalidActivityQuery()
		}
		cursor = decoded
	}
	return limit, cursor, nil
}

func invalidActivityQuery() *appError {
	return &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_query", Message: "ledger activity query must match the contract"}}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) *appError {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_content_type", Message: "Content-Type must be application/json"}}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_json", Message: "request body must match the contract"}}
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return &appError{Status: http.StatusBadRequest, Body: APIError{Code: "invalid_json", Message: "request body must contain one JSON object"}}
	}
	return nil
}
