package sqlite

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestK2CExecutionAuthorityFailsClosedAndBlocksDirectBypass(t *testing.T) {
	svc, path := testService(t, nil, nil)
	now := mustTime("2026-01-10T15:00:00Z")
	svc.now = func() time.Time { return now }
	ctx := context.Background()
	order := mustRecordK2AOrder(t, svc, "client-authority")

	if _, err := svc.AppendOrderEvent(ctx, k2aEvent("direct-risk", order.OrderID, "RISK_APPROVED")); err == nil {
		t.Fatal("direct risk approval bypassed execution authority")
	}
	if _, err := svc.AppendOrderEvent(ctx, k2aEvent("direct-dispatch", order.OrderID, "SUBMIT_DISPATCHED")); err == nil {
		t.Fatal("direct submit dispatch bypassed execution authority")
	}
	if _, err := svc.AuthorizeSyntheticDispatch(ctx, order.OrderID, 1); err == nil {
		t.Fatal("missing kill-switch state authorized dispatch")
	}
	armed, err := svc.SetSyntheticExecutionArmed(ctx, order.AccountRef, true)
	if err != nil || !armed.Armed || armed.LeaseOwner != "" {
		t.Fatalf("arm failed closed incorrectly: state=%+v err=%v", armed, err)
	}
	if _, err := svc.AuthorizeSyntheticDispatch(ctx, order.OrderID, armed.FencingToken); err == nil {
		t.Fatal("armed guard without lease authorized dispatch")
	}
	lease, err := svc.AcquireSyntheticExecutionLease(ctx, order.AccountRef)
	if err != nil || !lease.Armed || lease.LeaseOwner != svc.executionOwner || lease.FencingToken <= armed.FencingToken {
		t.Fatalf("lease acquisition failed: state=%+v err=%v", lease, err)
	}
	if _, err := svc.AuthorizeSyntheticDispatch(ctx, order.OrderID, lease.FencingToken-1); err == nil {
		t.Fatal("stale fencing token authorized dispatch")
	}

	foreignDB, err := openExistingDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = foreignDB.Close() })
	foreign := newService(foreignDB, func() time.Time { return now }, randomID)
	if _, err := foreign.AuthorizeSyntheticDispatch(ctx, order.OrderID, lease.FencingToken); err == nil {
		t.Fatal("foreign execution owner used another process lease")
	}

	now = now.Add(syntheticExecutionLeaseTTL + time.Nanosecond)
	if _, err := svc.AuthorizeSyntheticDispatch(ctx, order.OrderID, lease.FencingToken); err == nil {
		t.Fatal("expired lease authorized dispatch")
	}
	lease, err = svc.AcquireSyntheticExecutionLease(ctx, order.AccountRef)
	if err != nil || lease.FencingToken <= armed.FencingToken {
		t.Fatalf("expired lease was not fenced on reacquire: state=%+v err=%v", lease, err)
	}
	dispatched, err := svc.AuthorizeSyntheticDispatch(ctx, order.OrderID, lease.FencingToken)
	if err != nil || dispatched.Status != "SUBMIT_UNKNOWN" {
		t.Fatalf("valid authority did not dispatch atomically: state=%+v err=%v", dispatched, err)
	}
	replayed, err := svc.AuthorizeSyntheticDispatch(ctx, order.OrderID, lease.FencingToken)
	if err != nil || !reflect.DeepEqual(dispatched, replayed) {
		t.Fatalf("authority replay changed dispatch: first=%+v replay=%+v err=%v", dispatched, replayed, err)
	}
	assertK2CAuthorityCounts(t, svc, 1, 1, 1)

	halted, err := svc.SetSyntheticExecutionArmed(ctx, order.AccountRef, false)
	if err != nil || halted.Armed || halted.FencingToken <= lease.FencingToken {
		t.Fatalf("halt did not invalidate the lease: state=%+v err=%v", halted, err)
	}
	blocked := mustRecordK2AOrder(t, svc, "client-after-halt")
	if _, err := svc.AuthorizeSyntheticDispatch(ctx, blocked.OrderID, lease.FencingToken); err == nil {
		t.Fatal("halted execution authority accepted an old lease")
	}
}

func TestK2CFixedBuyPolicyAndReservationRace(t *testing.T) {
	svc, path := testService(t, nil, nil)
	now := mustTime("2026-01-10T15:00:00Z")
	svc.now = func() time.Time { return now }
	lease := mustK2CLease(t, svc, k2aAccountRef)

	for name, mutate := range map[string]func(*OrderIntent){
		"sell":           func(intent *OrderIntent) { intent.Side = "SELL" },
		"symbol":         func(intent *OrderIntent) { intent.Symbol = "035420" },
		"quantity":       func(intent *OrderIntent) { intent.Quantity = "11" },
		"order-notional": func(intent *OrderIntent) { intent.Quantity, intent.LimitPrice = "10", "100001" },
	} {
		t.Run(name, func(t *testing.T) {
			intent := k2aIntent("client-policy-" + name)
			mutate(&intent)
			state, err := svc.RecordOrderIntent(context.Background(), intent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AuthorizeSyntheticDispatch(context.Background(), state.OrderID, lease.FencingToken); err == nil {
				t.Fatal("fixed BUY policy accepted out-of-policy intent")
			}
		})
	}

	baselineIntent := k2aIntent("client-cap-baseline")
	baselineIntent.Quantity, baselineIntent.LimitPrice = "10", "60000"
	baseline, err := svc.RecordOrderIntent(context.Background(), baselineIntent)
	if err != nil {
		t.Fatal(err)
	}
	baseline = mustAuthorizeK2C(t, svc, baseline.OrderID, lease.FencingToken)
	ack := k2aEvent("cap-baseline-ack", baseline.OrderID, "SUBMIT_ACKNOWLEDGED")
	ack.ProviderOrderRef = "kiwoom_order_MMMMMMMMMMMMMMMMMMMMMMMM"
	mustAppendK2AEvent(t, svc, ack, "OPEN")

	first := recordK2COrder(t, svc, "client-cap-first", "4", "100000")
	second := recordK2COrder(t, svc, "client-cap-second", "4", "100000")
	otherDB, err := openExistingDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherDB.Close() })
	other := newService(otherDB, func() time.Time { return now }, randomID)
	other.executionOwner = svc.executionOwner

	states := make([]*OrderState, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, attempt := range []struct {
		service *Store
		orderID string
	}{{svc, first.OrderID}, {other, second.OrderID}} {
		wg.Add(1)
		go func(index int, attempt struct {
			service *Store
			orderID string
		}) {
			defer wg.Done()
			<-start
			states[index], errs[index] = attempt.service.AuthorizeSyntheticDispatch(context.Background(), attempt.orderID, lease.FencingToken)
		}(i, attempt)
	}
	close(start)
	wg.Wait()
	winner, loser := -1, -1
	for i := range errs {
		if errs[i] == nil {
			winner = i
		} else {
			loser = i
		}
	}
	if winner < 0 || loser < 0 || states[winner].Status != "SUBMIT_UNKNOWN" {
		t.Fatalf("reservation race did not admit exactly one order: states=%+v errors=%v", states, errs)
	}
	winnerOrder := []string{first.OrderID, second.OrderID}[winner]
	loserOrder := []string{first.OrderID, second.OrderID}[loser]
	winnerAck := k2aEvent("cap-winner-ack", winnerOrder, "SUBMIT_ACKNOWLEDGED")
	winnerAck.ProviderOrderRef = "kiwoom_order_NNNNNNNNNNNNNNNNNNNNNNNN"
	mustAppendK2AEvent(t, svc, winnerAck, "OPEN")
	if _, err := svc.AuthorizeSyntheticDispatch(context.Background(), loserOrder, lease.FencingToken); err == nil {
		t.Fatal("aggregate active BUY reservation cap was exceeded")
	}
	mustAppendK2AEvent(t, svc, k2aEvent("cap-winner-cancel", winnerOrder, "CANCEL_DISPATCHED"), "CANCEL_UNKNOWN")
	mustAppendK2AEvent(t, svc, k2aEvent("cap-winner-canceled", winnerOrder, "CANCEL_ACKNOWLEDGED"), "CANCELED")
	if state := mustAuthorizeK2C(t, svc, loserOrder, lease.FencingToken); state.Status != "SUBMIT_UNKNOWN" {
		t.Fatalf("terminal order did not release active reservation capacity: %+v", state)
	}
	assertK2CAuthorityCounts(t, svc, 3, 3, 3)
}

func mustK2CLease(t *testing.T, svc *Store, accountRef string) *ExecutionAuthorityState {
	t.Helper()
	if _, err := svc.SetSyntheticExecutionArmed(context.Background(), accountRef, true); err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireSyntheticExecutionLease(context.Background(), accountRef)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func recordK2COrder(t *testing.T, svc *Store, clientOrderID, quantity, price string) *OrderState {
	t.Helper()
	intent := k2aIntent(clientOrderID)
	intent.Quantity, intent.LimitPrice = quantity, price
	state, err := svc.RecordOrderIntent(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func mustAuthorizeK2C(t *testing.T, svc *Store, orderID string, fencingToken int64) *OrderState {
	t.Helper()
	state, err := svc.AuthorizeSyntheticDispatch(context.Background(), orderID, fencingToken)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func assertK2CAuthorityCounts(t *testing.T, svc *Store, reservations, approvals, dispatches int) {
	t.Helper()
	var gotReservations, gotApprovals, gotDispatches int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM risk_reservations`).Scan(&gotReservations); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_events WHERE event_type='RISK_APPROVED'`).Scan(&gotApprovals); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_events WHERE event_type='SUBMIT_DISPATCHED'`).Scan(&gotDispatches); err != nil {
		t.Fatal(err)
	}
	if gotReservations != reservations || gotApprovals != approvals || gotDispatches != dispatches {
		t.Fatalf("authority counts=(%d,%d,%d), want (%d,%d,%d)", gotReservations, gotApprovals, gotDispatches, reservations, approvals, dispatches)
	}
}

func TestK2CAuthorityLeaseRaceAndAtomicRollback(t *testing.T) {
	svc, path := testService(t, nil, nil)
	now := mustTime("2026-01-10T15:00:00Z")
	svc.now = func() time.Time { return now }
	if _, err := svc.SetSyntheticExecutionArmed(context.Background(), k2aAccountRef, true); err != nil {
		t.Fatal(err)
	}
	otherDB, err := openExistingDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherDB.Close() })
	other := newService(otherDB, func() time.Time { return now }, randomID)
	if other.executionOwner == svc.executionOwner {
		t.Fatal("independent services reused an execution owner")
	}

	services := []*Store{svc, other}
	states := make([]*ExecutionAuthorityState, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range services {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			states[index], errs[index] = services[index].AcquireSyntheticExecutionLease(context.Background(), k2aAccountRef)
		}(i)
	}
	close(start)
	wg.Wait()
	winner := -1
	for i := range errs {
		if errs[i] == nil {
			if winner != -1 {
				t.Fatalf("two process owners acquired one lease: states=%+v", states)
			}
			winner = i
		}
	}
	if winner == -1 || errs[1-winner] == nil {
		t.Fatalf("lease race did not admit exactly one owner: states=%+v errors=%v", states, errs)
	}

	winnerService := services[winner]
	order := mustRecordK2AOrder(t, winnerService, "client-atomic-rollback")
	var intentEventID string
	if err := winnerService.db.QueryRow(`SELECT event_id FROM order_events WHERE order_id=? AND event_type='INTENT_RECORDED'`, order.OrderID).Scan(&intentEventID); err != nil {
		t.Fatal(err)
	}
	orderEventCalls := 0
	winnerService.id = func(prefix string) string {
		switch prefix {
		case "risk_reservation":
			return "risk_reservation_atomic"
		case "order_event":
			orderEventCalls++
			if orderEventCalls == 1 {
				return "risk_event_atomic"
			}
			return intentEventID
		default:
			return prefix + "_atomic"
		}
	}
	if _, err := winnerService.AuthorizeSyntheticDispatch(context.Background(), order.OrderID, states[winner].FencingToken); err == nil {
		t.Fatal("dispatch event collision did not roll back authority transaction")
	}
	assertK2CAuthorityCounts(t, winnerService, 0, 0, 0)
	loaded, err := winnerService.LoadOrderState(context.Background(), order.OrderID)
	if err != nil || loaded.Status != "RECORDED" {
		t.Fatalf("failed authority transaction mutated order: state=%+v err=%v", loaded, err)
	}
}

func TestK2CRuntimeRejectsAuthorizedEventHashCorruption(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	order := mustRecordK2AOrder(t, svc, "client-authority-corruption")
	mustAuthorizeK2C(t, svc, order.OrderID, lease.FencingToken)
	if _, err := svc.db.Exec(`DROP TRIGGER order_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE order_events SET event_sha256=? WHERE order_id=? AND event_type='RISK_APPROVED'`,
		strings.Repeat("0", 64), order.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthorizeSyntheticDispatch(context.Background(), order.OrderID, lease.FencingToken); err == nil {
		t.Fatal("runtime replay accepted a corrupt authorized event hash")
	}
}
