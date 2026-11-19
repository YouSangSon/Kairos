package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValuationEvidenceHTTPAvailable(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/portfolio/valuation-evidence", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("valuation evidence status %d want 200: %s", w.Code, w.Body.String())
	}
	var empty struct {
		ConversionFX []json.RawMessage `json:"conversion_fx"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil || empty.ConversionFX == nil || len(empty.ConversionFX) != 0 {
		t.Fatalf("empty account must return an empty FX array: %s (%v)", w.Body.String(), err)
	}
}

func TestValuationEvidenceHTTPShapeAndPrivacy(t *testing.T) {
	svc := holdingValuationService(t)
	recordHoldingValuationPrice(t, svc, "private-price-receipt", "XNAS", "15", "2026-01-10T14:00:00Z", "2026-01-10T14:00:01Z", "2026-01-11T13:00:00Z")
	recordCurrentPortfolioFX(t, svc, "private-fx-source-id", "USD", "KRW", "1370", "2026-01-11T13:00:00Z")
	svc.now = func() time.Time { return mustTime(holdingValuationAsOf) }
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/portfolio/valuation-evidence", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["schema_version"] != "portfolio-valuation-evidence.v1" || body["source_availability_at_cutoff"] != "unverified" || body["observation_cutoff"] != holdingValuationAsOf {
		t.Fatal(body)
	}
	for _, part := range []string{"cash", "holdings", "current"} {
		view := body[part].(map[string]any)
		if view["valuation_as_of"] != holdingValuationAsOf || view["ledger_revision"] != "rev_0000000001" {
			t.Fatalf("mixed %s: %v", part, view)
		}
	}
	for field, path := range map[string]string{
		"cash":     "cash-valuation?base_currency=KRW&as_of=" + holdingValuationAsOf,
		"holdings": "holding-valuation?as_of=" + holdingValuationAsOf,
		"current":  "current-valuation",
	} {
		standalone := httptest.NewRecorder()
		svc.routes().ServeHTTP(standalone, httptest.NewRequest(http.MethodGet, "/v1/portfolio/"+path, nil))
		var view map[string]any
		if err := json.Unmarshal(standalone.Body.Bytes(), &view); err != nil ||
			standalone.Code != http.StatusOK || !reflect.DeepEqual(body[field], view) {
			t.Fatalf("%s differs from its standalone response: %s", field, standalone.Body.String())
		}
	}
	line := body["holdings"].(map[string]any)["lines"].([]any)[0].(map[string]any)
	if line["currency"] != "USD" || line["quantity"] != "3" || line["cost_basis"] != "31" || line["market_value"] != "45" || body["current"].(map[string]any)["total_value"].(map[string]any)["amount"] != "19180" {
		t.Fatal(body)
	}
	cashFX := body["cash"].(map[string]any)["lines"].([]any)[0].(map[string]any)["fx"].(map[string]any)
	fx := body["conversion_fx"].([]any)
	receiptID, _ := cashFX["observation_id"].(string)
	if len(fx) != 1 || cashFX["rate"] != "1370" || receiptID == "" ||
		!reflect.DeepEqual(fx[0], cashFX) {
		t.Fatalf("conversion_fx = %v, want one selected cash and holding receipt %v", fx, cashFX)
	}
	for _, private := range []string{"instrument_id", "private-price-receipt", "private-fx-source-id", "source_observation_id", "account_id", "record_sha256"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("private field exposed: %s", private)
		}
	}
	if _, ok := line["price"].(map[string]any)["observation_id"]; ok {
		t.Fatal("holding price ID exposed")
	}
	raw, err := os.ReadFile("../../contracts/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	path := doc["paths"].(map[string]any)["/v1/portfolio/valuation-evidence"].(map[string]any)
	operation := path["get"].(map[string]any)
	if len(path) != 1 || len(operation["parameters"].([]any)) != 0 {
		t.Fatal("evidence must be GET-only without parameters")
	}
	schema := doc["components"].(map[string]any)["schemas"].(map[string]any)["PortfolioValuationEvidence"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	if schema["additionalProperties"] != false || len(properties) != len(body) || !reflect.DeepEqual(jsonStringSet(schema["required"]), jsonKeySet(body)) {
		t.Fatal("evidence schema differs from closed runtime envelope")
	}
	for field, ref := range map[string]string{"cash": "CashValuation", "holdings": "HoldingValuation", "current": "CurrentPortfolioValuation"} {
		if properties[field].(map[string]any)["$ref"] != "#/components/schemas/"+ref {
			t.Fatal(field)
		}
	}
	if properties["conversion_fx"].(map[string]any)["items"].(map[string]any)["$ref"] != "#/components/schemas/CashValuationFX" {
		t.Fatal("conversion FX contract changed")
	}
}

func TestValuationEvidenceHTTPBoundaryAndServerFailure(t *testing.T) {
	svc := holdingValuationService(t)
	svc.now = func() time.Time { return mustTime(holdingValuationAsOf) }
	for _, query := range []string{"?as_of=private-query", "?%ZZ", "?x=y;x=z"} {
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/portfolio/valuation-evidence"+query, nil))
		if w.Code != 400 || strings.Contains(w.Body.String(), "private-query") || !strings.Contains(w.Body.String(), `"code":"invalid_query"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/portfolio/valuation-evidence", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	svc.now = func() time.Time { return mustTime("2020-01-01T00:00:00Z") }
	w = httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/portfolio/valuation-evidence", nil))
	if w.Code != 500 || strings.TrimSpace(w.Body.String()) != `{"code":"internal_error","message":"internal server error"}` {
		t.Fatalf("server cutoff must be redacted500: %d %s", w.Code, w.Body.String())
	}
}
