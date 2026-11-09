package ledger_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	ledger "omni-folio/services/core/internal/modules/ledger"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
)

func testLedger(t *testing.T) (contracts.Service, *sql.DB, *observation.Registry) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "ledger.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"001_init.sql", "003_broker_snapshots.sql", "005_ledger_events.sql", "008_cash_void.sql", "009_fx_exchange.sql", "022_ledger_replay_policy.sql"} {
		script, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Only ledger owner migrations are needed by this module fixture.
	for version := 1; version <= 27; version++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, version, "2026-01-10T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	now := func() time.Time { return time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC) }
	registry := observation.New(db, now)
	serial := 0
	api, err := ledger.Compose(ledger.Config{DB: db, Now: now, ID: func(prefix string) string { serial++; return fmt.Sprintf("%s_%d", prefix, serial) }, PreviewTTL: func() time.Duration { return 15 * time.Minute }, Observations: registry})
	if err != nil {
		t.Fatal(err)
	}
	return api, db, registry
}

func TestObservationFactsKeepProofSplitsAndRestatedFlows(t *testing.T) {
	api, db, registry := testLedger(t)
	input := []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,corrects_source_event_id\n" +
		"deposit,account-main,DEPOSIT,2026-01-01T00:00:00Z,,,,,USD,100,\n" +
		"buy,account-main,BUY,2026-01-02T00:00:00Z,AAPL,1,10,0,USD,-10,\n" +
		"split,account-main,SPLIT,2026-01-03T00:00:00.1Z,AAPL,2,,,USD,0,\n")
	preview, issue := api.PreviewImport(context.Background(), input)
	if issue != nil {
		t.Fatal(issue)
	}
	receipt, issue := api.ApplyImport(context.Background(), contracts.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: "first"})
	if issue != nil {
		t.Fatal(issue)
	}
	repeated, issue := api.ApplyImport(context.Background(), contracts.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: "first"})
	if issue != nil || !reflect.DeepEqual(receipt, repeated) {
		t.Fatalf("receipt retry changed: %+v %v", repeated, issue)
	}
	_, issue = api.ApplyImport(context.Background(), contracts.ApplyRequest{PreviewID: "different", IdempotencyKey: "first"})
	if issue == nil || issue.Kind != contracts.Conflict {
		t.Fatalf("idempotency mismatch accepted: %v", issue)
	}
	reversal := []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,corrects_source_event_id\nvoid,account-main,CASH_VOID,2026-01-04T00:00:00Z,,,,,USD,-100,deposit\n")
	preview, issue = api.PreviewImport(context.Background(), reversal)
	if issue != nil {
		t.Fatal(issue)
	}
	if _, issue = api.ApplyImport(context.Background(), contracts.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: "void"}); issue != nil {
		t.Fatal(issue)
	}
	var escaped storage.ObservationRef
	if err := registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		escaped = ref
		facts, err := api.ValuationFacts(context.Background(), ref)
		if err != nil {
			return err
		}
		admitted, err := api.AdmittedPriceIdentities(context.Background(), ref)
		if err != nil {
			return err
		}
		if facts.Snapshot.LedgerRevision != "rev_0000000004" || admitted.LedgerRevision != facts.Snapshot.LedgerRevision || facts.FutureRecorded || len(facts.RestatedEvents) != 2 || len(facts.ExternalFlows) != 0 || len(facts.LastSplits) != 1 || facts.LastSplits[0].OccurredAt.Format(time.RFC3339Nano) != "2026-01-03T00:00:00.1Z" || facts.AsOf.Format(time.RFC3339Nano) != "2026-01-04T00:00:00Z" {
			t.Fatalf("facts changed: %+v %+v", facts, admitted)
		}
		if !reflect.DeepEqual(admitted.Identities, []contracts.PriceIdentity{{InstrumentID: "instrument_aapl", Currency: "USD"}}) {
			t.Fatalf("admitted identities: %+v", admitted)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.ValuationFacts(context.Background(), escaped); err == nil {
		t.Fatal("expired observation reused")
	}
	if _, err := api.AdmittedPriceIdentities(context.Background(), storage.ObservationRef{}); err == nil {
		t.Fatal("zero observation accepted")
	}
	earlyRegistry := observation.New(db, func() time.Time { return time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC) })
	earlyAPI, err := ledger.Compose(ledger.Config{DB: db, Observations: earlyRegistry})
	if err != nil {
		t.Fatal(err)
	}
	if err := earlyRegistry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		facts, err := earlyAPI.ValuationFacts(context.Background(), ref)
		if err == nil && !facts.FutureRecorded {
			t.Fatal("future recorded event was hidden")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	foreign, _, other := testLedger(t)
	if err := other.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		if _, err := api.ValuationFacts(context.Background(), ref); err == nil {
			t.Fatal("foreign observation accepted")
		}
		if _, err := foreign.ValuationFacts(context.Background(), ref); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBrokerSnapshotAndReconciliationCommitTogether(t *testing.T) {
	api, db, _ := testLedger(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_test_reconciliation BEFORE INSERT ON broker_snapshot_reconciliations BEGIN SELECT RAISE(ABORT,'injected reconciliation failure'); END`); err != nil {
		t.Fatal(err)
	}
	snapshot := &contracts.BrokerSnapshot{Source: "kiwoom", Environment: "mock", Exchange: "KRX", AccountRef: "kiwoom_account_012345678901234567890123", MaskedAccount: "******1234", FetchedAt: "2026-01-10T14:59:00Z", Complete: true, Currency: "KRW", Totals: contracts.BrokerTotals{PurchaseAmount: "0", EvaluationAmount: "0", UnrealizedPnL: "0", ReturnRatePercent: "0", EstimatedAssets: "0", LoanAmount: "0", CreditLoanAmount: "0", CreditLendingAmount: "0"}, Positions: []contracts.BrokerPosition{}, OpenOrders: []contracts.BrokerOpenOrder{}}
	if _, err := api.RecordBrokerSnapshot(context.Background(), "account-main", snapshot); err == nil {
		t.Fatal("reconciliation failure succeeded")
	}
	var snapshots, reconciliations int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM broker_snapshots),(SELECT COUNT(*) FROM broker_snapshot_reconciliations)`).Scan(&snapshots, &reconciliations); err != nil {
		t.Fatal(err)
	}
	if snapshots != 0 || reconciliations != 0 {
		t.Fatalf("partial broker write survived: %d %d", snapshots, reconciliations)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_test_reconciliation`); err != nil {
		t.Fatal(err)
	}
	if err := api.VerifyBrokerSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := api.CheckBrokerBinding(context.Background(), snapshot.AccountRef); err != nil {
		t.Fatal(err)
	}
	first, err := api.RecordBrokerSnapshot(context.Background(), "account-main", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	again, err := api.RecordMockBrokerSnapshot(context.Background(), "account-main", snapshot.AccountRef, snapshot)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("snapshot retry changed: %+v %v", again, err)
	}
}

func TestPublicLedgerPreservesGoldenWireAndFingerprint(t *testing.T) {
	_, db, _ := testLedger(t)
	times := []string{"2026-01-10T15:00:30Z", "2026-01-10T15:01:00Z", "2026-01-10T15:01:00.125Z"}
	clockIndex := 0
	now := func() time.Time {
		value, err := time.Parse(time.RFC3339Nano, times[clockIndex])
		if err != nil {
			t.Fatal(err)
		}
		clockIndex++
		return value
	}
	ids := map[string][]string{"event": {"event_cash_001", "event_trade_001", "event_trade_002"}, "preview": {"preview_golden_001"}, "receipt": {"receipt_golden_001"}}
	id := func(prefix string) string { value := ids[prefix][0]; ids[prefix] = ids[prefix][1:]; return value }
	api, err := ledger.Compose(ledger.Config{DB: db, Now: now, ID: id, PreviewTTL: func() time.Duration { return 15 * time.Minute }, Observations: observation.New(db, now)})
	if err != nil {
		t.Fatal(err)
	}
	fixture := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "contracts", "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	assertFixture := func(actual any, name string) {
		t.Helper()
		encoded, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		if err := json.Compact(&expected, fixture(name)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, expected.Bytes()) {
			t.Fatalf("wire bytes differ from %s\n%s", name, encoded)
		}
	}
	preview, issue := api.PreviewImport(context.Background(), fixture("golden-import.csv"))
	if issue != nil {
		t.Fatal(issue)
	}
	assertFixture(preview, "golden-preview.json")
	var request contracts.ApplyRequest
	if err := json.Unmarshal(fixture("golden-apply-request.json"), &request); err != nil {
		t.Fatal(err)
	}
	receipt, issue := api.ApplyImport(context.Background(), request)
	if issue != nil {
		t.Fatal(issue)
	}
	assertFixture(receipt, "golden-apply.json")
	snapshot, err := api.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertFixture(snapshot, "golden-snapshot.json")
}

func TestComposeRejectsForeignDatabaseRegistry(t *testing.T) {
	_, first, firstRegistry := testLedger(t)
	_, second, _ := testLedger(t)
	for _, config := range []ledger.Config{{DB: first}, {DB: second, Observations: firstRegistry}} {
		if _, err := ledger.Compose(config); err == nil {
			t.Fatal("unbound observation database accepted")
		}
	}
}

func TestBackupArchivalUsesLegacyFIFOWithCurrentReplayState(t *testing.T) {
	api, _, registry := testLedger(t)
	raw := []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount\nwhole,account-main,BUY,2026-01-01T00:00:00Z,AAPL,1,10,0,USD,-10\nfractional,account-main,BUY,2026-01-01T00:00:00.1Z,AAPL,1,20,0,USD,-20\nsell,account-main,SELL,2026-01-02T00:00:00Z,AAPL,1,30,0,USD,30\n")
	preview, issue := api.PreviewImport(context.Background(), raw)
	if issue != nil {
		t.Fatal(issue)
	}
	if _, issue := api.ApplyImport(context.Background(), contracts.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: "archival"}); issue != nil {
		t.Fatal(issue)
	}
	if err := registry.VerifyBackup(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		current, err := api.VerifyBackup(context.Background(), ref, false)
		if err != nil {
			return err
		}
		archival, err := api.VerifyBackup(context.Background(), ref, true)
		if err != nil {
			return err
		}
		if len(current.Snapshot.Holdings) != 1 || current.Snapshot.Holdings[0].CostBasis != "20" {
			t.Fatalf("current FIFO changed: %+v", current.Snapshot)
		}
		if len(archival.Snapshot.Holdings) != 1 || archival.Snapshot.Holdings[0].CostBasis != "10" || archival.Snapshot.CostBasisPolicy != "fifo_exact_else_half_even_residual_8_v1" {
			t.Fatalf("archival replay adopted current FIFO: %+v", archival.Snapshot)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
