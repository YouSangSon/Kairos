package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	k2aAccountRef = "kiwoom_account_AAAAAAAAAAAAAAAAAAAAAAAA"
	k2aOrderRef   = "kiwoom_order_BBBBBBBBBBBBBBBBBBBBBBBB"
)

func TestSchemaMigratesV1ToLatestAndReadinessRequiresLatest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	script, err := migrationFiles.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)`, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events(sequence,event_id,source_event_id,account_id,type,occurred_at,currency,amount,receipt_id,recorded_at) VALUES(1,'preserved','preserved','account-main','DEPOSIT','2026-01-01T00:00:00Z','KRW','1','receipt','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var version, migrations, preserved int
	if err := db.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_migrations`).Scan(&version, &migrations); err != nil {
		t.Fatal(err)
	}
	if version != latestSchema || migrations != latestSchema {
		t.Fatalf("schema version=(%d,%d), want latest=%d with complete migration history", version, migrations, latestSchema)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE event_id='preserved'`).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("v1 data was not preserved: count=%d err=%v", preserved, err)
	}
	var preservedTarget string
	if err := db.QueryRow(`SELECT COALESCE(corrects_source_event_id, '') FROM events WHERE event_id='preserved'`).Scan(&preservedTarget); err != nil || preservedTarget != "" {
		t.Fatalf("v1 event gained a correction target: target=%q err=%v", preservedTarget, err)
	}
	var strict, guard int
	if err := db.QueryRow(`SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='events'`).Scan(&strict); err != nil || strict != 1 {
		t.Fatalf("v9 events table is not strict: strict=%d err=%v", strict, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name='events_cash_void_guard'`).Scan(&guard); err != nil || guard != 1 {
		t.Fatalf("v9 cash-void guard is missing: count=%d err=%v", guard, err)
	}
	for _, table := range []string{"order_idempotency", "order_events", "execution_authority_events", "risk_reservations", "broker_snapshots", "broker_snapshot_reconciliations", "strategy_research_evidence", "strategy_selection_events", "paper_evaluation_events"} {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil || exists != 1 {
			t.Fatalf("migration did not create %s: exists=%d err=%v", table, exists, err)
		}
	}
	var fxStrict int
	if err := db.QueryRow(`SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='fx_observations'`).Scan(&fxStrict); err != nil || fxStrict != 1 {
		t.Fatalf("v10 FX observation table is not strict: strict=%d err=%v", fxStrict, err)
	}
	var securityPriceStrict int
	if err := db.QueryRow(`SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='security_price_observations'`).Scan(&securityPriceStrict); err != nil || securityPriceStrict != 1 {
		t.Fatalf("v12 security price observation table is not strict: strict=%d err=%v", securityPriceStrict, err)
	}
	if err := migrate(db); err != nil {
		t.Fatal("repeated migration was not idempotent:", err)
	}

	svc := newService(db, time.Now, randomID)
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("current schema was not ready: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPaperMigrationRejectsForeignKeyDamageAtomically(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "damaged-v6.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	files := []string{"001_init.sql", "002_orders.sql", "003_broker_snapshots.sql", "004_execution_authority.sql", "005_ledger_events.sql", "006_strategy_registry.sql"}
	for version, name := range files {
		script, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version+1, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_events(sequence,event_id,event_sha256,order_id,event_type,source,event_json,recorded_at) VALUES(1,'orphan-event','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','missing-order','INTENT_RECORDED','synthetic','{}','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err == nil || !strings.Contains(err.Error(), "foreign-key check failed") {
		t.Fatalf("damaged v6 migration was accepted: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 6 {
		t.Fatalf("failed migration was not rolled back: version=%d err=%v", version, err)
	}
}

func k2aIntent(clientOrderID string) OrderIntent {
	return OrderIntent{
		ClientOrderID: clientOrderID,
		Provider:      "kiwoom",
		Mode:          "synthetic",
		AccountRef:    k2aAccountRef,
		Symbol:        "005930",
		Exchange:      "KRX",
		Side:          "BUY",
		OrderType:     "LIMIT",
		Quantity:      "10",
		LimitPrice:    "1000",
		Currency:      "KRW",
	}
}

func k2aEvent(eventID, orderID, eventType string) OrderEvent {
	return OrderEvent{EventID: eventID, OrderID: orderID, Type: eventType, Source: "synthetic"}
}

func k2aFill(eventID, orderID, executionRef, quantity, price string) OrderEvent {
	event := k2aEvent(eventID, orderID, "FILL_RECORDED")
	event.ProviderOrderRef = k2aOrderRef
	event.ProviderExecutionRef = executionRef
	event.Quantity = quantity
	event.Price = price
	event.OccurredAt = "2026-01-10T15:01:00Z"
	return event
}

func mustRecordK2AOrder(t *testing.T, svc *Service, clientOrderID string) *OrderState {
	t.Helper()
	state, err := svc.recordOrderIntent(context.Background(), k2aIntent(clientOrderID))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func mustReadyAndDispatchK2AOrder(t *testing.T, svc *Service, clientOrderID, eventPrefix string) *OrderState {
	t.Helper()
	_ = eventPrefix
	state := mustRecordK2AOrder(t, svc, clientOrderID)
	lease := mustK2CLease(t, svc, state.AccountRef)
	return mustAuthorizeK2C(t, svc, state.OrderID, lease.FencingToken)
}

func mustAppendK2AEvent(t *testing.T, svc *Service, event OrderEvent, status string) *OrderState {
	t.Helper()
	state, err := svc.appendOrderEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != status {
		t.Fatalf("event %s produced status %s, want %s: %+v", event.Type, state.Status, status, state)
	}
	return state
}
