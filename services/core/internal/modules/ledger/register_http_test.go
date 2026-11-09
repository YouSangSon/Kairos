package ledger_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ledger "omni-folio/services/core/internal/modules/ledger"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
)

func TestHTTPPublicLedgerImportAndOpaquePagination(t *testing.T) {
	api, _, _ := testLedger(t)
	mux := http.NewServeMux()
	ledger.RegisterHTTP(mux, api, [32]byte{})
	request := func(method, path, media, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if media != "" {
			r.Header.Set("Content-Type", media)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	preview := request("POST", "/v1/imports/preview", "text/csv", "source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount\nfirst,account-main,DEPOSIT,2025-01-01T00:00:00Z,,,,,USD,1\nsecond,account-main,DEPOSIT,2025-01-02T00:00:00Z,,,,,USD,2\n")
	var p c.ImportPreview
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil || preview.Code != 200 || !p.CanApply {
		t.Fatalf("preview=%s err=%v", preview.Body.String(), err)
	}
	body, _ := json.Marshal(c.ApplyRequest{PreviewID: p.PreviewID, IdempotencyKey: "http-seed"})
	apply := request("POST", "/v1/imports/apply", "application/json", string(body))
	if apply.Code != 200 {
		t.Fatal(apply.Body.String())
	}
	page := request("GET", "/v1/ledger/activities?limit=1", "", "")
	var first c.LedgerActivityPage
	if err := json.Unmarshal(page.Body.Bytes(), &first); err != nil || page.Code != 200 || len(first.Events) != 1 || first.Events[0].Amount != "2" || first.NextCursor == nil {
		t.Fatalf("page=%s err=%v", page.Body.String(), err)
	}
	if strings.Contains(*first.NextCursor, "ledger_revision") {
		t.Fatal("cursor exposed private pagination fields")
	}
	next := request("GET", "/v1/ledger/activities?limit=1&cursor="+url.QueryEscape(*first.NextCursor), "", "")
	var second c.LedgerActivityPage
	if err := json.Unmarshal(next.Body.Bytes(), &second); err != nil || next.Code != 200 || len(second.Events) != 1 || second.Events[0].Amount != "1" || second.NextCursor != nil {
		t.Fatalf("next=%s err=%v", next.Body.String(), err)
	}
	for _, path := range []string{"/v1/ledger/activities?limit=01", "/v1/ledger/activities?limit=1&limit=2", "/v1/ledger/activities?cursor=invalid", "/v1/ledger/activities?extra=1", "/v1/portfolio/instrument-ledger?instrument_id=instrument_aapl&currency=USD&extra=1"} {
		if got := request("GET", path, "", ""); got.Code != 400 {
			t.Fatalf("accepted %s: %d", path, got.Code)
		}
	}
	for _, tc := range []struct{ media, body, code string }{{"text/plain", "{}", "invalid_content_type"}, {"application/json", `{"unknown":true}`, "invalid_json"}, {"application/json", "{}{}", "invalid_json"}} {
		got := request("POST", "/v1/imports/apply", tc.media, tc.body)
		if got.Code != 400 || !strings.Contains(got.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("decode response drifted: %s", got.Body.String())
		}
	}
	if got := request("POST", "/v1/imports/preview", "text/csv", strings.Repeat("x", (1<<20)+1)); got.Code != 400 || !strings.Contains(got.Body.String(), `"code":"invalid_body"`) {
		t.Fatalf("body limit drifted: %s", got.Body.String())
	}
	if got := request("POST", "/v1/portfolio/snapshot", "", ""); got.Code != 405 {
		t.Fatalf("method status=%d", got.Code)
	}
	if got := request("GET", "/other", "", ""); got.Code != 404 {
		t.Fatalf("unknown path status=%d", got.Code)
	}
}

func TestHTTPStatusStaysLightweightAndPrivate(t *testing.T) {
	api, db, _ := testLedger(t)
	mux := http.NewServeMux()
	ledger.RegisterHTTP(mux, api, [32]byte{})
	get := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	want := "{\"service\":\"omni-folio\",\"live_enabled\":false,\"mode\":\"local_import_only\",\"trust_state\":\"never_verified\",\"ledger_revision\":\"rev_0000000000\",\"last_verified_at\":null,\"issues\":[]}\n"
	if got := get("GET", "/v1/status"); got.Code != 200 || got.Body.String() != want || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status wire drifted: %s", got.Body.String())
	}
	if _, err := db.Exec(`UPDATE ledger_meta SET revision=7; DROP TABLE events`); err != nil {
		t.Fatal(err)
	}
	if got := get("GET", "/v1/status"); got.Code != 200 || !strings.Contains(got.Body.String(), "rev_0000000007") {
		t.Fatalf("status unexpectedly requires ledger replay: %s", got.Body.String())
	}
	if got := get("GET", "/v1/portfolio/snapshot"); got.Code != 500 || got.Body.String() != "{\"code\":\"internal_error\",\"message\":\"internal server error\"}\n" {
		t.Fatalf("snapshot leaked failure: %s", got.Body.String())
	}
	if got := get("HEAD", "/v1/status"); got.Code != 200 {
		t.Fatalf("HEAD status=%d", got.Code)
	}
}
