package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFXObservationBackupProof(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	if _, err := svc.recordFXObservation(context.Background(), FXObservationInput{
		Source: "local_fixture", SourceObservationID: "fx_obs_backup", BaseCurrency: "USD", QuoteCurrency: "KRW",
		Rate: "1370", ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z",
	}); err != nil {
		t.Fatal(err)
	}
	golden := writeCurrentSnapshot(t, svc.db)
	backup := t.TempDir() + "/fx.db"
	manifestPath := backup + ".manifest.json"
	manifest, err := createBackup(svc.db, backup, golden, manifestPath, svc.now, svc.id)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != backupFormat || manifest.SchemaVersion != backupSchema ||
		manifest.FXObservationCount != 1 || len(manifest.FXObservationStateSHA256) != 64 ||
		manifest.VerificationReceipt.FXObservationCheck != "ok" ||
		manifest.VerificationReceipt.CandidateFXObservationStateSHA256 != manifest.FXObservationStateSHA256 {
		t.Fatalf("backup omitted FX observation proof: %+v", manifest)
	}
	if err := verifyManifest(backup, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	tampered := readJSONMap(t, manifestPath)
	tampered["fx_observation_state_sha256"] = strings.Repeat("0", 64)
	tamperedPath := t.TempDir() + "/tampered-manifest.json"
	writeJSONFile(t, tamperedPath, tampered)
	if err := verifyManifest(backup, golden, tamperedPath); err == nil {
		t.Fatal("manifest with a different FX observation digest was accepted")
	}
}

func TestFXObservationLatestDirectReadIsExactAndSanitized(t *testing.T) {
	svc, _ := testService(t, nil, map[string][]string{"fx_observation": {"fx_observation_001", "fx_observation_002"}})
	for _, input := range []FXObservationInput{
		{Source: "local_fixture", SourceObservationID: "provider-1", BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: "1370", ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z"},
		{Source: "local_fixture", SourceObservationID: "provider-2", BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: "1380", ObservedAt: "2026-01-10T15:01:00Z", FetchedAt: "2026-01-10T15:01:01Z"},
	} {
		if _, err := svc.recordFXObservation(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	request := func(path string) (int, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return w.Code, body
	}
	status, body := request("/v1/market-data/fx/latest?source=local_fixture&base_currency=USD&quote_currency=KRW&as_of=2026-01-10T15:00:30Z")
	if status != http.StatusOK || body["observation_id"] != "fx_observation_001" || body["rate"] != "1370" ||
		body["sample"] != true || body["state"] != "stale" || body["source_observation_id"] != nil {
		t.Fatalf("unexpected direct FX response: status=%d body=%#v", status, body)
	}
	unsafe := map[string]int{
		"/v1/market-data/fx/latest?source=local_fixture&base_currency=KRW&quote_currency=USD&as_of=2026-01-10T15:00:30Z":                      http.StatusNotFound,
		"/v1/market-data/fx/latest?source=local_fixture&base_currency=USD&quote_currency=KRW":                                                 http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=local_fixture&source=local_fixture&base_currency=USD&quote_currency=KRW&as_of=2026-01-10T15:00:30Z": http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=local_fixture&base_currency=USD&quote_currency=KRW&as_of=2026-01-10T15:00:30Z&inverse=true":         http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=kiwoom&base_currency=USD&quote_currency=KRW&as_of=2026-01-10T15:00:30Z":                             http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=local_fixture&base_currency=usd&quote_currency=KRW&as_of=2026-01-10T15:00:30Z":                      http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=local_fixture&base_currency=USD&quote_currency=USD&as_of=2026-01-10T15:00:30Z":                      http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=local_fixture&base_currency=USD&quote_currency=KRW&as_of=2026-01-11T00:00:30%2B09:00":               http.StatusBadRequest,
		"/v1/market-data/fx/latest?source=local_fixture;base_currency=USD&quote_currency=KRW&as_of=2026-01-10T15:00:30Z":                      http.StatusBadRequest,
	}
	for path, wantStatus := range unsafe {
		status, _ := request(path)
		if status != wantStatus {
			t.Fatalf("unsafe FX query status=%d want=%d path=%s", status, wantStatus, path)
		}
	}
}

func TestFXObservationOpenAPIIsClosedReadOnlyAndDirect(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	path := document["paths"].(map[string]any)["/v1/market-data/fx/latest"].(map[string]any)
	if len(path) != 1 || path["get"] == nil {
		t.Fatal("FX observation route must be GET-only")
	}
	schema := document["components"].(map[string]any)["schemas"].(map[string]any)["LocalFixtureFXObservation"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatal("FX observation response must be closed")
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"source_observation_id", "account_id", "portfolio_value", "inverse_rate", "cross_rate"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("FX observation contract exposes %s", forbidden)
		}
	}
}
