package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"omni-folio/services/core/internal/codec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPaperPromotionReportHTTPBoundaryAndSharedEmptyFixture(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-10-27T15:00:00Z") }
	w := paperPromotionReportRequestForTest(t, svc, http.MethodGet, "/v1/paper/promotion-report", http.StatusOK)
	assertPaperPromotionReportCase(t, w.Body.Bytes(), "empty")
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodHead, "/v1/paper/promotion-report", http.StatusOK},
		{http.MethodPost, "/v1/paper/promotion-report", http.StatusMethodNotAllowed},
		{http.MethodPut, "/v1/paper/promotion-report", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/v1/paper/promotion-report", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/paper/promotion-report?account=private-account", http.StatusBadRequest},
		{http.MethodGet, "/v1/paper/promotion-report?as_of=2026-01-01", http.StatusBadRequest},
	} {
		w := paperPromotionReportRequestForTest(t, svc, tc.method, tc.path, tc.status)
		if tc.status == http.StatusBadRequest {
			var body APIError
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != "invalid_query" || strings.Contains(w.Body.String(), "private-account") {
				t.Fatalf("query contract: %s, %v", w.Body.String(), err)
			}
		}
	}
	const origin = "http://localhost:8081"
	request := httptest.NewRequest(http.MethodOptions, "/v1/paper/promotion-report", nil)
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	w = httptest.NewRecorder()
	withCORS(svc.routes(), origin).ServeHTTP(w, request)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != origin || w.Body.Len() != 0 {
		t.Fatalf("existing CORS preflight: %d %s", w.Code, w.Body.String())
	}
}

type paperPromotionReportStateForTestValue struct {
	Changes                                 int64
	Claims, Decisions, Selection, Authority string
	Schema                                  string
}

func paperPromotionReportStateForTest(t *testing.T, svc *Service) paperPromotionReportStateForTestValue {
	t.Helper()
	var state paperPromotionReportStateForTestValue
	err := svc.db.QueryRow(`SELECT total_changes(),
		(SELECT json_group_array(record_json) FROM (SELECT record_json FROM paper_promotion_claims ORDER BY sequence)),
		(SELECT json_group_array(record_json) FROM (SELECT record_json FROM paper_promotion_decisions ORDER BY sequence)),
		(SELECT json_group_array(record_json) FROM (SELECT record_json FROM strategy_selection_events ORDER BY sequence)),
		(SELECT json_group_array(record_json) FROM (SELECT record_json FROM execution_authority_events ORDER BY sequence)),
		(SELECT json_group_array(json_array(type,name,tbl_name,sql)) FROM (SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name))`).Scan(&state.Changes, &state.Claims, &state.Decisions, &state.Selection, &state.Authority, &state.Schema)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func paperPromotionReportRequestForTest(t *testing.T, svc *Service, method, path string, status int) *httptest.ResponseRecorder {
	t.Helper()
	before := paperPromotionReportStateForTest(t, svc)
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(method, path, nil))
	if after := paperPromotionReportStateForTest(t, svc); after != before {
		t.Fatal("report changed database journal, current selection, or execution authority")
	}
	if w.Code != status {
		t.Fatalf("%s %s: status=%d body=%s", method, path, w.Code, w.Body.String())
	}
	if status != http.StatusMethodNotAllowed && w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("report response must not be cached")
	}
	return w
}

func paperPromotionReportCaseForTest(t *testing.T, name string) json.RawMessage {
	t.Helper()
	var cases struct {
		Cases []struct {
			Name     string          `json:"name"`
			Response json.RawMessage `json:"response"`
		} `json:"cases"`
	}
	decodeFixture(t, "paper-promotion-report-cases.json", &cases)
	for _, tc := range cases.Cases {
		if tc.Name == name {
			return tc.Response
		}
	}
	t.Fatalf("missing shared paper promotion report case %q", name)
	return nil
}

func assertPaperPromotionReportCase(t *testing.T, raw []byte, name string) {
	t.Helper()
	want := decodePaperPromotionReportForTest(t, paperPromotionReportCaseForTest(t, name))
	got := decodePaperPromotionReportForTest(t, raw)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("report differs from shared %s fixture: got=%s want=%s", name, raw, paperPromotionReportCaseForTest(t, name))
	}
}

func decodePaperPromotionReportForTest(t *testing.T, raw []byte) *PaperPromotionReportView {
	t.Helper()
	var view PaperPromotionReportView
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&view); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("report has trailing JSON: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if !hasExactKeysForTest(body, "schema_version", "mode", "observed_at", "attempt") || view.SchemaVersion != "paper-promotion-report.v1" || view.Mode != "paper_comparison_only" || !codec.CanonicalUTC(view.ObservedAt) {
		t.Fatalf("invalid closed report root: %s", raw)
	}
	if view.Attempt == nil {
		return &view
	}
	attempt := body["attempt"].(map[string]any)
	if !hasExactKeysForTest(attempt, "symbol", "claimed_at", "decided_at", "comparison_start", "comparison_end", "candidate", "incumbent", "decision", "reason_code", "promotion_selection_status", "comparison") {
		t.Fatalf("invalid closed attempt: %v", attempt)
	}
	for _, field := range []string{"candidate", "incumbent"} {
		if !hasExactKeysForTest(attempt[field].(map[string]any), "fast_window", "slow_window", "quantity") {
			t.Fatalf("invalid closed %s strategy", field)
		}
	}
	if view.Attempt.Comparison == nil {
		return &view
	}
	comparison := attempt["comparison"].(map[string]any)
	if !hasExactKeysForTest(comparison, "policy_version", "metrics_verification", "decision", "reason_code", "windows") || len(view.Attempt.Comparison.Windows) != 3 {
		t.Fatalf("invalid closed comparison: %v", comparison)
	}
	for _, value := range comparison["windows"].([]any) {
		window := value.(map[string]any)
		if !hasExactKeysForTest(window, "start", "end", "candidate", "incumbent", "buy_and_hold") {
			t.Fatalf("invalid closed comparison window: %v", window)
		}
		for _, field := range []string{"candidate", "incumbent", "buy_and_hold"} {
			if !hasExactKeysForTest(window[field].(map[string]any), "after_cost_return", "max_drawdown", "round_trips", "turnover") {
				t.Fatalf("invalid closed %s metrics", field)
			}
		}
	}
	return &view
}
