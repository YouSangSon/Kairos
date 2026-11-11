package ledger_test

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"omni-folio/services/core/internal/modules/ledger"
	"strings"
	"testing"
)

func TestLedgerHTTPInternalFailureLogsCauseWithoutExposingIt(t *testing.T) {
	api, db, _ := testLedger(t)
	if _, err := db.Exec(`DROP TABLE ledger_meta`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	mux := http.NewServeMux()
	ledger.RegisterHTTP(mux, api, [32]byte{})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/portfolio/snapshot", nil))
	if response.Code != 500 || strings.Contains(response.Body.String(), "ledger_meta") {
		t.Fatalf("failure was exposed: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(output.String(), "ledger_meta") {
		t.Fatalf("internal cause was lost: %s", output.String())
	}
}
