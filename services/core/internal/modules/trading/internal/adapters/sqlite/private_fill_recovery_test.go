package sqlite

import (
	"context"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestG38C2PaperAccountingExactBuyAndSell(t *testing.T) {
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	svc.now = func() time.Time { return mustTime("2026-01-15T07:00:00Z") }
	signal.TargetQuantity, signal.ExpiresAt = "2", "2026-01-20T00:00:00.000000000Z"
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, buy, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	buyBar := recordG38C2FillBar(t, svc, "fixture-005930-buy", "100", "5", "2026-01-10")
	buy, err = svc.RunPaperOrder(context.Background(), buy.OrderID, lease.FencingToken)
	if err != nil || buy.Status != "FILLED" || buy.FilledQuantity != "2" {
		t.Fatalf("BUY state=%+v err=%v", buy, err)
	}
	states, err := replayPaperAccounting(context.Background(), svc.db)
	wantBuy := paperAccountState{
		AccountRef: k2aAccountRef, Cash: "9798.8", Fees: "1", Taxes: "0", Slippage: "0.2", RealizedPnL: "0", CapitalizedFills: 1,
		Lots: map[string][]paperLotState{"005930": {{Quantity: "2", Cost: "201.2"}}},
	}
	wantBuy.PaperAccountingSessionID = states[k2aAccountRef].PaperAccountingSessionID
	if err != nil || !samePaperAccountState(states[k2aAccountRef], wantBuy) {
		t.Fatalf("BUY accounting=%+v err=%v", states[k2aAccountRef], err)
	}

	sellSignal := signal
	sellSignal.SignalID, sellSignal.TargetQuantity = "g38c2-sell-all", "0"
	sellSignal.SignalBarObservationID, sellSignal.DataSHA256, sellSignal.DataAsOf = buyBar.ObservationID, buyBar.InputDataSHA256, buyBar.CloseAt
	sellSignal.GeneratedAt = buyBar.SourceAvailableAt
	_, sell, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, sellSignal, lease.FencingToken)
	if err != nil || sell == nil || orderSideForTest(sell, t, svc) != "SELL" || sell.Quantity != "2" {
		t.Fatalf("SELL order=%+v err=%v", sell, err)
	}
	recordG38C2FillBar(t, svc, "fixture-005930-sell", "120", "5", "2026-01-11")
	sell, err = svc.RunPaperOrder(context.Background(), sell.OrderID, lease.FencingToken)
	if err != nil || sell.Status != "FILLED" {
		t.Fatalf("SELL state=%+v err=%v", sell, err)
	}
	states, err = replayPaperAccounting(context.Background(), svc.db)
	wantSell := paperAccountState{
		AccountRef: k2aAccountRef, PaperAccountingSessionID: wantBuy.PaperAccountingSessionID,
		Cash: "10037.32024", Lots: map[string][]paperLotState{}, Fees: "2", Taxes: "0.23976",
		Slippage: "0.44", RealizedPnL: "37.32024", CapitalizedFills: 2,
	}
	if err != nil || !samePaperAccountState(states[k2aAccountRef], wantSell) {
		t.Fatalf("SELL accounting=%+v err=%v", states[k2aAccountRef], err)
	}
	proof, err := provePaperAccountingRecovery(context.Background(), svc.db)
	if err != nil || proof.Sessions != 1 || proof.MarketBars != 3 || proof.Signals != 2 || proof.Authorizations != 2 ||
		proof.CapitalizedFills != 2 || proof.SHA256 == "" {
		t.Fatalf("accounting proof=%+v err=%v", proof, err)
	}
	var ledgerEvents, reservations, fillTables int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&ledgerEvents); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM risk_reservations`).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE '%paper%fill%'`).Scan(&fillTables); err != nil {
		t.Fatal(err)
	}
	if ledgerEvents != 0 || reservations != 0 || fillTables != 0 {
		t.Fatalf("paper accounting escaped its journal: ledger=%d reservations=%d fill_tables=%d", ledgerEvents, reservations, fillTables)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "paper-fills.db")
	manifest, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: backup + ".manifest.json"})
	if err != nil || manifest.PaperAccountingStateSHA256 != proof.SHA256 ||
		manifest.VerificationReceipt.CandidatePaperAccountingStateSHA256 != proof.SHA256 ||
		manifest.PaperAccountingSessionCount != 1 || manifest.PaperMarketBarObservationCount != 3 ||
		manifest.PaperSignalEventCount != 2 || manifest.PaperExecutionAuthorizationCount != 2 || manifest.PaperCapitalizedFillCount != 2 {
		t.Fatalf("fill backup manifest=%+v err=%v", manifest, err)
	}
	restored, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredStates, err := replayPaperAccounting(context.Background(), &database{restored})
	if err != nil || !samePaperAccountState(restoredStates[k2aAccountRef], wantSell) {
		t.Fatalf("restored accounting=%+v err=%v", restoredStates[k2aAccountRef], err)
	}
}

func TestG38C2PaperFillRawSQLThreatBoundaryFailsRecoveryAndActivation(t *testing.T) {
	svc, forged := g38c2ForgedPaperFill(t, 1)
	golden := mixedWriteCurrentSnapshot(t, svc)
	forged.Price = "101"
	tx, err := svc.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertOrderEvent(context.Background(), tx, forged, svc.now().UTC().Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		t.Fatalf("structurally complete raw fill did not reach the documented recovery boundary: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := replayPaperAccounting(context.Background(), svc.db); err == nil {
		t.Fatal("recovery accepted raw SQL fill with economically wrong price")
	}
	candidate := filepath.Join(t.TempDir(), "raw-sql-wrong-price.db")
	if _, err := svc.db.Exec(`VACUUM INTO '` + strings.ReplaceAll(candidate, "'", "''") + `'`); err != nil {
		t.Fatal(err)
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyRestore(context.Background(), candidate, golden); err == nil {
		t.Fatal("restore activation accepted raw SQL fill with economically wrong price")
	}
}
