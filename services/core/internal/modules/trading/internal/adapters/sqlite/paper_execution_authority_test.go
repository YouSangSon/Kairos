package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type cancelPaperAuthorizationRead struct {
	orderQuerier
	table  string
	cancel context.CancelFunc
}

func (q cancelPaperAuthorizationRead) QueryRowContext(ctx context.Context, query string, args ...any) rowScanner {
	if strings.Contains(query, "FROM "+q.table) {
		q.cancel()
	}
	return q.orderQuerier.QueryRowContext(ctx, query, args...)
}

func TestPaperAuthorizationReadCancellationPreservesCause(t *testing.T) {
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	signal.TargetQuantity = "10"
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
	if err != nil || order == nil {
		t.Fatalf("admit: %v", err)
	}
	authorization, found, err := loadPaperExecutionAuthorizationByOrder(context.Background(), svc.db, order.OrderID)
	if err != nil || !found {
		t.Fatalf("authorization: %v", err)
	}
	before := paperAdmissionCountsForTest(t, svc)
	for _, table := range []string{"paper_accounting_sessions", "execution_authority_events"} {
		t.Run(table, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			q := cancelPaperAuthorizationRead{orderQuerier: svc.db, table: table, cancel: cancel}
			state, err := validatePaperExecutionAuthorization(ctx, q, authorization)
			if ctx.Err() == nil || !errors.Is(err, context.Canceled) || state != nil {
				t.Fatalf("cancellation became integrity error: state=%+v err=%v", state, err)
			}
		})
	}
	if _, err := provePaperPerformancePolicyRecovery(context.Background(), svc.db); err != nil || paperAdmissionCountsForTest(t, svc) != before {
		t.Fatalf("read cancellation changed durable history: %v", err)
	}
}

func TestG38C2PaperIntentBinding(t *testing.T) {
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	signal.TargetQuantity = "10"
	lease := mustK2CLease(t, svc, k2aAccountRef)
	event, state, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
	if err != nil || event == nil || state == nil || state.Status != "OPEN" {
		t.Fatalf("capitalized order admission event=%+v state=%+v err=%v", event, state, err)
	}
	intent, err := loadOrderIntentFrom(context.Background(), svc.db, state.OrderID)
	if err != nil || intent.OrderType != "PAPER_MARKET" || intent.LimitPrice != "" || intent.Side != "BUY" || intent.Quantity != "10" ||
		intent.PaperAccountingSessionID != event.PaperAccountingSessionID || intent.PaperAccountingPolicyVersion != paperAccountingPolicyVersion ||
		intent.PaperSignalEventID != event.EventID || intent.ExecutionPolicySHA256 != event.ExecutionPolicySHA256 {
		t.Fatalf("capitalized intent=%+v event=%+v err=%v", intent, event, err)
	}
	var reservations, fills int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM risk_reservations`).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_events WHERE event_type='FILL_RECORDED'`).Scan(&fills); err != nil {
		t.Fatal(err)
	}
	if reservations != 0 || fills != 0 {
		t.Fatalf("paper admission reused synthetic accounting: reservations=%d fills=%d", reservations, fills)
	}

	for _, test := range []struct {
		name   string
		mutate func(*OrderIntent)
	}{
		{name: "missing schema", mutate: func(intent *OrderIntent) { intent.SignalSchemaVersion = "" }},
		{name: "unknown schema", mutate: func(intent *OrderIntent) { intent.SignalSchemaVersion = "paper-signal.v4" }},
		{name: "session", mutate: func(intent *OrderIntent) { intent.PaperAccountingSessionID = "paper_accounting_session_wrong" }},
		{name: "signal", mutate: func(intent *OrderIntent) { intent.PaperSignalEventID = "paper_signal_event_wrong" }},
		{name: "account", mutate: func(intent *OrderIntent) { intent.AccountRef = "kiwoom_account_BBBBBBBBBBBBBBBBBBBBBBBB" }},
		{name: "symbol", mutate: func(intent *OrderIntent) { intent.Symbol = "000660" }},
		{name: "target", mutate: func(intent *OrderIntent) { intent.SignalTargetQuantity = "11" }},
		{name: "selection", mutate: func(intent *OrderIntent) { intent.StrategySelectionEventID = "selection_wrong" }},
		{name: "side", mutate: func(intent *OrderIntent) { intent.Side = "SELL" }},
		{name: "quantity", mutate: func(intent *OrderIntent) { intent.Quantity = "9" }},
		{name: "policy", mutate: func(intent *OrderIntent) { intent.ExecutionPolicySHA256 = strings.Repeat("0", 64) }},
	} {
		t.Run("direct "+test.name, func(t *testing.T) {
			candidateSvc, candidateSignal, _ := g38c2PaperSignalFixture(t, true)
			candidateSignal.TargetQuantity = "10"
			signalEvent, err := recordG38C2PaperSignalForTest(candidateSvc, k2aAccountRef, candidateSignal)
			if err != nil {
				t.Fatal(err)
			}
			candidate := capitalizedPaperOrderIntent(*signalEvent, "BUY", "10")
			candidate.ClientOrderID = "paper_direct_" + strings.ReplaceAll(test.name, " ", "_")
			test.mutate(&candidate)
			if err := insertPaperOrderIntentDirectForTest(candidateSvc, candidate, "order_direct_"+test.name); err == nil {
				t.Fatal("direct capitalized intent mismatch was accepted")
			}
			var count int
			if err := candidateSvc.db.QueryRow(`SELECT COUNT(*) FROM order_idempotency`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected direct intent rows=%d err=%v", count, err)
			}
		})
	}

	for _, test := range []struct {
		name, target string
		quantity     any
	}{
		{name: "leading zero quantity", target: "10", quantity: "010"},
		{name: "suffix junk quantity", target: "10", quantity: "10x"},
		{name: "numeric JSON quantity", target: "10", quantity: float64(10)},
		{name: "oversized quantity", target: paperMaxQuantity, quantity: "4611686018427387904"},
		{name: "overflow numeric quantity", target: paperMaxQuantity, quantity: 1e40},
	} {
		t.Run("direct "+test.name, func(t *testing.T) {
			svc, signal, _ := g38c2PaperSignalFixture(t, true)
			signal.TargetQuantity = test.target
			signalEvent, err := recordG38C2PaperSignalForTest(svc, k2aAccountRef, signal)
			if err != nil {
				t.Fatal(err)
			}
			candidate := capitalizedPaperOrderIntent(*signalEvent, "BUY", test.target)
			var raw map[string]any
			encoded, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &raw); err != nil {
				t.Fatal(err)
			}
			raw["quantity"] = test.quantity
			if err := insertPaperOrderIntentJSONDirectForTest(svc, candidate, raw, "order_direct_"+strings.ReplaceAll(test.name, " ", "_")); err == nil {
				t.Fatal("direct capitalized intent accepted a non-canonical quantity")
			}
			var count int
			if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_idempotency`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected direct quantity rows=%d err=%v", count, err)
			}
		})
	}
}

func TestG38C2PaperTarget(t *testing.T) {
	t.Run("buy retry zero and active ceiling", func(t *testing.T) {
		svc, signal, _ := g38c2PaperSignalFixture(t, true)
		lease := mustK2CLease(t, svc, k2aAccountRef)
		signal.TargetQuantity = "10"
		firstEvent, firstOrder, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
		if err != nil || firstOrder == nil || firstOrder.Status != "OPEN" || firstOrder.Quantity != "10" {
			t.Fatalf("BUY target event=%+v order=%+v err=%v", firstEvent, firstOrder, err)
		}
		beforeRetry := paperAdmissionCountsForTest(t, svc)
		replayedEvent, replayedOrder, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
		if err != nil || replayedEvent.EventID != firstEvent.EventID || replayedOrder.OrderID != firstOrder.OrderID || paperAdmissionCountsForTest(t, svc) != beforeRetry {
			t.Fatalf("exact retry event=%+v order=%+v err=%v", replayedEvent, replayedOrder, err)
		}

		same := signal
		same.SignalID = "g38c2-same-active-target"
		if _, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, same, lease.FencingToken); err != nil || order != nil {
			t.Fatalf("same active target order=%+v err=%v", order, err)
		}
		beforeChanged := paperAdmissionCountsForTest(t, svc)
		changed := signal
		changed.SignalID, changed.TargetQuantity = "g38c2-changed-active-target", "11"
		if _, _, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, changed, lease.FencingToken); err == nil {
			t.Fatal("changed active target was accepted")
		}
		if after := paperAdmissionCountsForTest(t, svc); after != beforeChanged {
			t.Fatalf("changed active target leaked rows: before=%+v after=%+v", beforeChanged, after)
		}
	})

	t.Run("zero target records only signal", func(t *testing.T) {
		svc, signal, _ := g38c2PaperSignalFixture(t, true)
		lease := mustK2CLease(t, svc, k2aAccountRef)
		event, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
		counts := paperAdmissionCountsForTest(t, svc)
		if err != nil || event == nil || order != nil || counts.Signals != 1 || counts.Orders != 0 || counts.Authorizations != 0 || counts.Events != 0 {
			t.Fatalf("zero target event=%+v order=%+v counts=%+v err=%v", event, order, counts, err)
		}
	})

	t.Run("session admits later current strategy with the same execution policy", func(t *testing.T) {
		svc, signal, _ := g38c2PaperSignalFixture(t, true)
		session, found, err := loadPaperAccountingSession(context.Background(), svc.db, k2aAccountRef)
		if err != nil || !found {
			t.Fatalf("accounting session=%+v found=%t err=%v", session, found, err)
		}
		next, err := svc.registerStrategyEvidence(context.Background(), strategyArtifact(t, func(config map[string]any) {
			config["experiment_id"] = "g38c2-same-policy-current-strategy"
			config["strategy"].(map[string]any)["version"] = "1.0.8"
		}))
		if err != nil {
			t.Fatal(err)
		}
		selected, err := svc.SelectPaperCandidate(context.Background(), next.ResultSHA256, signal.StrategySelectionEventID)
		if err != nil {
			t.Fatal(err)
		}
		signal.SignalID = "g38c2-same-policy-later-strategy"
		signal.StrategyResultSHA256 = next.ResultSHA256
		signal.StrategySelectionEventID = selected.CurrentEventID
		lease := mustK2CLease(t, svc, k2aAccountRef)
		event, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
		if err != nil || event == nil || order != nil || event.PaperAccountingSessionID != session.SessionID ||
			event.ExecutionPolicySHA256 != session.ExecutionPolicySHA256 {
			t.Fatalf("later strategy event=%+v order=%+v session=%+v err=%v", event, order, session, err)
		}
	})

	t.Run("concurrent admissions create one active order", func(t *testing.T) {
		primary, signal, _ := g38c2PaperSignalFixture(t, true)
		fixedNow := primary.now().UTC()
		primary.now = func() time.Time { return fixedNow }
		var databasePath string
		if err := primary.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&databasePath); err != nil {
			t.Fatal(err)
		}
		secondDB, err := openExistingDB(databasePath)
		if err != nil {
			t.Fatal(err)
		}
		defer secondDB.Close()
		secondary := newService(secondDB, func() time.Time { return fixedNow }, func(prefix string) string { return prefix + "_concurrent_secondary" })
		secondary.executionOwner = primary.executionOwner
		lease := mustK2CLease(t, primary, k2aAccountRef)
		primary.id = func(prefix string) string { return prefix + "_concurrent_primary" }
		signal.TargetQuantity = "10"
		first, second := signal, signal
		first.SignalID, second.SignalID = "g38c2-concurrent-first", "g38c2-concurrent-second"
		type result struct {
			order *OrderState
			err   error
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		for service, candidate := range map[*Store]PaperSignal{primary: first, secondary: second} {
			service, candidate := service, candidate
			go func() {
				<-start
				_, order, err := service.AdmitPaperSignal(context.Background(), k2aAccountRef, candidate, lease.FencingToken)
				results <- result{order: order, err: err}
			}()
		}
		close(start)
		orders := 0
		for range 2 {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.order != nil {
				orders++
			}
		}
		counts := paperAdmissionCountsForTest(t, primary)
		if orders != 1 || counts.Signals != 2 || counts.Orders != 1 || counts.Authorizations != 1 || counts.Events != 4 || counts.Fills != 0 {
			t.Fatalf("concurrent admissions orders=%d counts=%+v", orders, counts)
		}
	})
}

type paperAdmissionCounts struct {
	Signals, Orders, Authorizations, Events, Fills int
}

func paperAdmissionCountsForTest(t testing.TB, svc *Store) paperAdmissionCounts {
	t.Helper()
	var counts paperAdmissionCounts
	for query, destination := range map[string]*int{
		`SELECT COUNT(*) FROM paper_signal_events`:                           &counts.Signals,
		`SELECT COUNT(*) FROM order_idempotency WHERE mode='paper'`:          &counts.Orders,
		`SELECT COUNT(*) FROM paper_execution_authorizations`:                &counts.Authorizations,
		`SELECT COUNT(*) FROM order_events`:                                  &counts.Events,
		`SELECT COUNT(*) FROM order_events WHERE event_type='FILL_RECORDED'`: &counts.Fills,
	} {
		if err := svc.db.QueryRow(query).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	return counts
}

func insertPaperOrderIntentDirectForTest(svc *Store, intent OrderIntent, orderID string) error {
	raw, hash, err := orderJSONHash(intent)
	if err != nil {
		return err
	}
	_, err = svc.db.Exec(`INSERT INTO order_idempotency(provider,mode,account_ref,client_order_id,request_sha256,order_id,intent_json,recorded_at)
		VALUES(?,?,?,?,?,?,?,?)`, intent.Provider, intent.Mode, intent.AccountRef, intent.ClientOrderID, hash, orderID, string(raw), svc.now().UTC().Format(time.RFC3339Nano))
	return err
}

func insertPaperOrderIntentJSONDirectForTest(svc *Store, intent OrderIntent, raw map[string]any, orderID string) error {
	encoded, hash, err := orderJSONHash(raw)
	if err != nil {
		return err
	}
	_, err = svc.db.Exec(`INSERT INTO order_idempotency(provider,mode,account_ref,client_order_id,request_sha256,order_id,intent_json,recorded_at)
		VALUES(?,?,?,?,?,?,?,?)`, intent.Provider, intent.Mode, intent.AccountRef, intent.ClientOrderID, hash, orderID, string(encoded), svc.now().UTC().Format(time.RFC3339Nano))
	return err
}
