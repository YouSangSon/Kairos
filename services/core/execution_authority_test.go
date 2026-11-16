package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func mustK2CLease(t *testing.T, svc *Service, accountRef string) *ExecutionAuthorityState {
	t.Helper()
	if _, err := svc.setSyntheticExecutionArmed(context.Background(), accountRef, true); err != nil {
		t.Fatal(err)
	}
	lease, err := svc.acquireSyntheticExecutionLease(context.Background(), accountRef)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func mustAuthorizeK2C(t *testing.T, svc *Service, orderID string, fencingToken int64) *OrderState {
	t.Helper()
	state, err := svc.authorizeSyntheticDispatch(context.Background(), orderID, fencingToken)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestK2CBackupRestoresAuthorityAndStartsWithNoOwnedLease(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	now := mustTime("2026-01-10T15:00:00Z")
	svc.now = func() time.Time { return now }
	lease := mustK2CLease(t, svc, k2aAccountRef)
	order := mustRecordK2AOrder(t, svc, "client-authority-backup")
	mustAuthorizeK2C(t, svc, order.OrderID, lease.FencingToken)
	golden := writeCurrentSnapshot(t, svc.db)
	backup := filepath.Join(t.TempDir(), "backup.db")
	manifest, err := createBackup(svc.db, backup, golden, backup+".manifest.json", func() time.Time { return now }, func(prefix string) string { return prefix + "_k2c" })
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != backupFormat || manifest.SchemaVersion != backupSchema ||
		manifest.ExecutionAuthorityEventCount != 2 || manifest.RiskReservationCount != 1 {
		t.Fatalf("backup omitted execution authority proof: %+v", manifest)
	}
	if err := verifyManifest(backup, golden, backup+".manifest.json"); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoredDB.Close() })
	restored := newService(restoredDB, func() time.Time { return now }, randomID)
	newOrder := mustRecordK2AOrder(t, restored, "client-restored-authority")
	if _, err := restored.authorizeSyntheticDispatch(context.Background(), newOrder.OrderID, lease.FencingToken); err == nil {
		t.Fatal("restored process reused another process lease")
	}
}

func TestK2CMigrationPreservesLegacyNonAuthoritativeOrderEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-v3.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for i, name := range []string{"001_init.sql", "002_orders.sql", "003_broker_snapshots.sql"} {
		script, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`, i+1, "2026-01-10T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	intent := k2aIntent("client-legacy-v3")
	intentJSON, intentSHA, err := orderJSONHash(intent)
	if err != nil {
		t.Fatal(err)
	}
	const orderID = "order_legacy_v3"
	if _, err := db.Exec(`INSERT INTO order_idempotency(provider,mode,account_ref,client_order_id,request_sha256,order_id,intent_json,recorded_at) VALUES(?,?,?,?,?,?,?,?)`,
		intent.Provider, intent.Mode, intent.AccountRef, intent.ClientOrderID, intentSHA, orderID, string(intentJSON), "2026-01-10T15:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for _, event := range []OrderEvent{
		k2aEvent("legacy-intent", orderID, "INTENT_RECORDED"),
		k2aEvent("legacy-risk", orderID, "RISK_APPROVED"),
		k2aEvent("legacy-dispatch", orderID, "SUBMIT_DISPATCHED"),
	} {
		raw, hash, err := orderJSONHash(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO order_events(event_id,event_sha256,order_id,event_type,source,event_json,recorded_at) VALUES(?,?,?,?,?,?,?)`,
			event.EventID, hash, event.OrderID, event.Type, event.Source, string(raw), "2026-01-10T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	state, err := newService(db, time.Now, randomID).loadOrderState(context.Background(), orderID)
	if err != nil || state.Status != "SUBMIT_UNKNOWN" || state.PendingAction != "SUBMIT" {
		t.Fatalf("v4 migration lost legacy unknown-submit state: state=%+v err=%v", state, err)
	}
	proof, err := proveOrderRecovery(context.Background(), db)
	if err != nil || proof.ExecutionAuthorityEvents != 0 || proof.RiskReservations != 0 {
		t.Fatalf("legacy events were incorrectly promoted to authority: proof=%+v err=%v", proof, err)
	}
}
