package marketdata_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"omni-folio/services/core/internal/composition/observations"
	market "omni-folio/services/core/internal/modules/marketdata"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
)

func TestMarketHTTPKeepsReviewAndApplyBoundaries(t *testing.T) {
	m, l, _, registry := marketFixture(t)
	api := market.ComposeOwnerPrice(&observations.OwnerPrice{Registry: registry, Ledger: l, Scoped: m.ScopedPrice}, l.CanonicalInstrumentID)
	mux := http.NewServeMux()
	market.RegisterHTTP(mux, m, api, func() c.CandleSource { return nil }, l.CanonicalInstrumentID, func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) })
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	preview := request("POST", "/v1/market-data/imports/preview", priceInput)
	if preview.Code != 200 || preview.Header().Get("Cache-Control") != "no-store" || preview.Body.Bytes()[preview.Body.Len()-1] == '\n' {
		t.Fatalf("review transport changed %d %q", preview.Code, preview.Body.String())
	}
	var result struct {
		Kind    string                     `json:"kind"`
		Summary map[string]json.RawMessage `json:"summary"`
		Input   string                     `json:"input"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "security_price" || result.Input != priceInput || result.Summary["storage_check"] != nil || result.Summary["recorded_at"] != nil {
		t.Fatalf("review claimed storage %+v", result)
	}
	apply := request("POST", "/v1/market-data/imports/apply", priceInput)
	if apply.Code != 400 || apply.Body.String() != "{\"code\":\"owner_data_invalid\",\"message\":\"owner data input does not meet the declared contract\"}\n" {
		t.Fatalf("unadmitted input changed %d %s", apply.Code, apply.Body.String())
	}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/v1/market-data/imports/preview?", priceInput, 400},
		{"POST", "/v1/market-data/imports/preview", `{"invalid":true}`, 400},
		{"GET", "/v1/market-data/candles?symbol=005930&interval=1d", "", 503},
		{"GET", "/v1/market-data/fx/latest?source=local_fixture&base_currency=USD&quote_currency=KRW&as_of=2026-01-01T00:00:00Z", "", 404},
		{"GET", "/v1/market-data/imports/preview", "", 405},
		{"GET", "/missing", "", 404},
	} {
		if w := request(test.method, test.path, test.body); w.Code != test.status {
			t.Errorf("%s %s = %d, want %d: %s", test.method, test.path, w.Code, test.status, w.Body.String())
		}
	}
}
