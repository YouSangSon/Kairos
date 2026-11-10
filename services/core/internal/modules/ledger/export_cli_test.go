package ledger_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"omni-folio/services/core/internal/modules/ledger"
	"omni-folio/services/core/internal/modules/ledger/construction"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	"os"
	"path/filepath"
	"testing"
)

type exportSession struct {
	api    c.Service
	closed *int
}

func (s exportSession) Ledger() c.Service    { return s.api }
func (s exportSession) RequireSchema() error { return nil }
func (s exportSession) Close() error         { *s.closed++; return nil }

type exportResources struct {
	session exportSession
	reads   *int
}

func (r exportResources) OpenExisting(string) (construction.Session, error) {
	return nil, errors.New("wrong open mode")
}
func (r exportResources) OpenReadWrite(context.Context, string) (construction.Session, error) {
	return nil, errors.New("wrong open mode")
}
func (r exportResources) OpenReadOnly(context.Context, string) (construction.Session, error) {
	*r.reads++
	return r.session, nil
}

type exportOutputFailure struct{}

func (exportOutputFailure) Write([]byte) (int, error) { return 0, errors.New("output failed") }
func TestExportCLIPublishesExactOwnerEventsAndRetainsPublishedOutputOnReceiptFailure(t *testing.T) {
	api, db, _ := testLedger(t)
	raw := []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount\nfirst,account-main,DEPOSIT,2025-01-01T00:00:00Z,,,,,USD,1\nsecond,account-main,DEPOSIT,2025-01-02T00:00:00Z,,,,,USD,2\n")
	p, issue := api.PreviewImport(context.Background(), raw)
	if issue != nil {
		t.Fatal(issue)
	}
	if _, issue := api.ApplyImport(context.Background(), c.ApplyRequest{PreviewID: p.PreviewID, IdempotencyKey: "export-cli"}); issue != nil {
		t.Fatal(issue)
	}
	var index int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&index, &name, &path); err != nil {
		t.Fatal(err)
	}
	closes, reads := 0, 0
	resources := exportResources{session: exportSession{api: api, closed: &closes}, reads: &reads}
	target := filepath.Join(t.TempDir(), "ledger.csv")
	var out bytes.Buffer
	if err := ledger.RunCLI(context.Background(), "ledger-export", []string{"-db", path, "-out", target}, ledger.CLIConfig{Resources: resources, Stderr: io.Discard}, &out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var receipt c.ExportReceipt
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if receipt.Rows != 2 || receipt.LedgerRevision != "rev_0000000002" || receipt.SHA256 != hex.EncodeToString(digest[:]) || closes != 1 || reads != 1 {
		t.Fatalf("receipt/lifetime drift: %+v close=%d read=%d", receipt, closes, reads)
	}
	want := "type,occurred_at,recorded_at,symbol,quantity,price,fee,currency,amount,counter_currency,counter_amount,is_correction\nDEPOSIT,2025-01-02T00:00:00Z,2026-01-10T15:00:00Z,,,,,USD,2,,,false\nDEPOSIT,2025-01-01T00:00:00Z,2026-01-10T15:00:00Z,,,,,USD,1,,,false\n"
	if string(data) != want {
		t.Fatalf("export bytes changed: %s", data)
	}
	failed := filepath.Join(t.TempDir(), "published-before-receipt-failure.csv")
	err = ledger.RunCLI(context.Background(), "ledger-export", []string{"-db", path, "-out", failed}, ledger.CLIConfig{Resources: resources, Stderr: io.Discard}, exportOutputFailure{})
	if err == nil || closes != 2 {
		t.Fatalf("receipt failure/lifetime: %v close=%d", err, closes)
	}
	if got, err := os.ReadFile(failed); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("published file lost on receipt failure: %v", err)
	}
}
