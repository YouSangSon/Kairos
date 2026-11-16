package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPaperProposalNoneRejectsSupersededStoredBar(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	raw := paperProposalForTest(t, svc, signal, "none")
	recordG38C2FillBar(t, svc, "newer-proposal-bar", "100", "100", "2026-01-10")
	if _, _, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, lease.FencingToken); err == nil {
		t.Fatal("stale no-signal proposal reported success")
	}
}

func paperProposalFixture(t *testing.T) (*Store, PaperSignal, *PaperMarketBarObservation) {
	t.Helper()
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:00Z") }
	evidence, selected := selectedPaperStrategy(t, svc)
	if _, err := svc.OpenPaperAccountingSession(context.Background(), k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(evidence.ArtifactJSON), &root); err != nil {
		t.Fatal(err)
	}
	params := root["manifest"].(map[string]any)["strategy"].(map[string]any)["parameters"].(map[string]any)
	slow := int(params["slow_window"].(float64))
	last := mustTime("2026-01-09T00:00:00Z")
	for i := slow; i > 0; i-- {
		day := last.AddDate(0, 0, -i).Format("2006-01-02")
		recordG38C2FillBar(t, svc, fmt.Sprintf("proposal-warmup-%d", i), "99", "100", day)
	}
	bar := recordG38C2FillBar(t, svc, "proposal-signal-bar", "100", "100", "2026-01-09")
	return svc, PaperSignal{SchemaVersion: capitalizedPaperSignalSchema, SignalID: "proposal-fixture", SignalBarObservationID: bar.ObservationID, StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID, DataSHA256: bar.InputDataSHA256, Symbol: bar.Symbol, DataAsOf: bar.CloseAt}, bar
}

func paperProposalForTest(t *testing.T, svc *Store, signal PaperSignal, kind string) []byte {
	t.Helper()
	var artifact string
	if err := svc.db.QueryRow(`SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, signal.StrategyResultSHA256).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	evidence, err := decodeStrategyArtifact([]byte(artifact))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(artifact), &root); err != nil {
		t.Fatal(err)
	}
	parameters := root["manifest"].(map[string]any)["strategy"].(map[string]any)["parameters"].(map[string]any)
	var quantity any = parameters["quantity"]
	if kind == "death_cross" {
		quantity = "0"
	}
	if kind == "none" {
		quantity = nil
	}
	body := map[string]any{
		"schema_version": "paper-signal-proposal.v1", "mode": "paper_proposal_only",
		"strategy_result_sha256": evidence.ResultSHA256, "strategy_parameter_sha256": evidence.ParameterSHA256,
		"input_sha256": signal.DataSHA256, "symbol": signal.Symbol, "data_as_of": strings.Replace(signal.DataAsOf, ".000000000Z", "Z", 1),
		"signal": kind, "target_quantity": quantity,
	}
	return hashPaperProposalForTest(t, body)
}

func TestPaperProposalAdmissionRejectsContractAndAuthorityDrift(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	raw := paperProposalForTest(t, svc, signal, "golden_cross")
	before := paperAdmissionCountsForTest(t, svc)
	for name, mutate := range map[string]func(map[string]any){
		"extra field":      func(p map[string]any) { p["account"] = k2aAccountRef },
		"parameter hash":   func(p map[string]any) { p["strategy_parameter_sha256"] = strings.Repeat("b", 64) },
		"result hash":      func(p map[string]any) { p["strategy_result_sha256"] = strings.Repeat("b", 64) },
		"input hash":       func(p map[string]any) { p["input_sha256"] = strings.Repeat("b", 64) },
		"symbol":           func(p map[string]any) { p["symbol"] = "000660" },
		"time":             func(p map[string]any) { p["data_as_of"] = "2026-01-09T06:29:00Z" },
		"target":           func(p map[string]any) { p["target_quantity"] = "11" },
		"numeric target":   func(p map[string]any) { p["target_quantity"] = 10 },
		"none liquidation": func(p map[string]any) { p["signal"] = "none"; p["target_quantity"] = "0" },
		"death buy":        func(p map[string]any) { p["signal"] = "death_cross" },
		"forged death":     func(p map[string]any) { p["signal"] = "death_cross"; p["target_quantity"] = "0" },
		"forged none":      func(p map[string]any) { p["signal"] = "none"; p["target_quantity"] = nil },
		"mode":             func(p map[string]any) { p["mode"] = "live" },
	} {
		t.Run(name, func(t *testing.T) {
			var p map[string]any
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			mutate(p)
			if _, _, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, hashPaperProposalForTest(t, p), signal.SignalBarObservationID, lease.FencingToken); err == nil {
				t.Fatal("accepted invalid proposal")
			}
			if paperAdmissionCountsForTest(t, svc) != before {
				t.Fatal("rejection left durable effects")
			}
		})
	}
	for _, bad := range [][]byte{[]byte("[]"), []byte("null"), append(append([]byte{}, raw...), []byte("{}")...), []byte(strings.Replace(string(raw), `"mode":`, `"mode":"paper_proposal_only","mode":`, 1))} {
		if _, err := decodePaperProposal(bad); err == nil {
			t.Fatal("accepted malformed or duplicate JSON")
		}
	}
	for _, fence := range []int64{0, lease.FencingToken + 1} {
		if _, _, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, fence); err == nil {
			t.Fatal("accepted invalid fence")
		}
	}
	if _, err := svc.RollbackPaperCandidate(context.Background(), signal.StrategySelectionEventID, signal.StrategySelectionEventID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, lease.FencingToken); err == nil {
		t.Fatal("accepted rolled-back selection")
	}
	if paperAdmissionCountsForTest(t, svc) != before {
		t.Fatal("authority rejection left orders or signals")
	}
}

func TestPaperProposalFailureAtomicityAndOwnedDeadline(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	raw := paperProposalForTest(t, svc, signal, "golden_cross")
	before := paperAdmissionCountsForTest(t, svc)
	foreign := newService(svc.db.DB, svc.now, randomID)
	if _, _, err := foreign.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, lease.FencingToken); err == nil {
		t.Fatal("foreign execution owner accepted")
	}
	if _, err := svc.db.Exec(`CREATE TRIGGER fail_proposal_order BEFORE INSERT ON order_idempotency BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, lease.FencingToken); err == nil {
		t.Fatal("injected order failure ignored")
	}
	if before != paperAdmissionCountsForTest(t, svc) {
		t.Fatal("order failure left an orphan signal or authorization")
	}
	if _, err := svc.db.Exec(`DROP TRIGGER fail_proposal_order`); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:30Z") }
	lease = mustK2CLease(t, svc, k2aAccountRef)
	if _, _, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, lease.FencingToken); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("receipt deadline not enforced: %v", err)
	}
	if before != paperAdmissionCountsForTest(t, svc) {
		t.Fatal("expired proposal left durable command")
	}
}

func TestPaperProposalReselectionReusesInitialCapital(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	var artifact string
	if err := svc.db.QueryRow(`SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, signal.StrategyResultSHA256).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	second := rehashedStrategyArtifact(t, []byte(artifact), func(a map[string]any) { a["experiment_id"] = "same-policy-followup" })
	evidence, err := svc.registerStrategyEvidence(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(context.Background(), evidence.ResultSHA256, signal.StrategySelectionEventID)
	if err != nil {
		t.Fatal(err)
	}
	initial, found, err := loadPaperAccountingSession(context.Background(), svc.db, k2aAccountRef)
	if err != nil || !found {
		t.Fatal("missing initial session")
	}
	signal.StrategyResultSHA256, signal.StrategySelectionEventID = evidence.ResultSHA256, selected.CurrentEventID
	lease := mustK2CLease(t, svc, k2aAccountRef)
	event, order, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, selected.CurrentEventID, paperProposalForTest(t, svc, signal, "golden_cross"), signal.SignalBarObservationID, lease.FencingToken)
	if err != nil || order == nil || event.PaperAccountingSessionID != initial.SessionID {
		t.Fatalf("followup replaced or rejected initial capital: %v", err)
	}
	states, err := replayPaperAccounting(context.Background(), svc.db)
	if err != nil || states[k2aAccountRef].Cash != "10000" || states[k2aAccountRef].PaperAccountingSessionID != initial.SessionID {
		t.Fatalf("initial capital changed: %v", err)
	}
}

func TestPaperProposalNonePreservesPositionAndDeathCreatesSell(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, buy, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, paperProposalForTest(t, svc, signal, "golden_cross"), signal.SignalBarObservationID, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	bar := recordG38C2FillBar(t, svc, "proposal-buy-fill", "101", "100", "2026-01-10")
	buy, err = svc.RunPaperOrder(context.Background(), buy.OrderID, lease.FencingToken)
	if err != nil || buy.Status != "FILLED" {
		t.Fatalf("fill=%+v err=%v", buy, err)
	}
	signal.SignalBarObservationID, signal.DataSHA256, signal.DataAsOf = bar.ObservationID, bar.InputDataSHA256, bar.CloseAt
	before := paperAdmissionCountsForTest(t, svc)
	event, order, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, paperProposalForTest(t, svc, signal, "none"), signal.SignalBarObservationID, lease.FencingToken)
	if err != nil || event != nil || order != nil || before != paperAdmissionCountsForTest(t, svc) {
		t.Fatalf("none created a command: %v", err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-11T07:00:00Z") }
	lease = mustK2CLease(t, svc, k2aAccountRef)
	bar = recordG38C2FillBar(t, svc, "proposal-death-bar", "1", "100", "2026-01-11")
	signal.SignalBarObservationID, signal.DataSHA256, signal.DataAsOf = bar.ObservationID, bar.InputDataSHA256, bar.CloseAt
	_, sell, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, paperProposalForTest(t, svc, signal, "death_cross"), signal.SignalBarObservationID, lease.FencingToken)
	if err != nil || sell == nil || sell.Quantity != "10" || orderSideForTest(sell, t, svc) != "SELL" {
		t.Fatalf("death failed to reduce filled position: %+v %v", sell, err)
	}
}

func TestPaperProposalCommittedNoDeltaNeverCreatesLaterOrder(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	ctx := context.Background()
	bar := recordG38C2FillBar(t, svc, "empty-position-death", "1", "100", "2026-01-10")
	signal.SignalBarObservationID, signal.DataSHA256, signal.DataAsOf = bar.ObservationID, bar.InputDataSHA256, bar.CloseAt
	raw := paperProposalForTest(t, svc, signal, "death_cross")
	lease := mustK2CLease(t, svc, k2aAccountRef)
	event, order, err := svc.AdmitPaperProposal(ctx, k2aAccountRef, signal.StrategySelectionEventID, raw, bar.ObservationID, lease.FencingToken)
	if err != nil || event == nil || order != nil {
		t.Fatalf("empty-position death must commit without an order: %v", err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-11T07:00:00Z") }
	lease = mustK2CLease(t, svc, k2aAccountRef)
	buyBar := recordG38C2FillBar(t, svc, "later-golden", "200", "100", "2026-01-11")
	buySignal := signal
	buySignal.SignalBarObservationID, buySignal.DataSHA256, buySignal.DataAsOf = buyBar.ObservationID, buyBar.InputDataSHA256, buyBar.CloseAt
	_, buy, err := svc.AdmitPaperProposal(ctx, k2aAccountRef, signal.StrategySelectionEventID, paperProposalForTest(t, svc, buySignal, "golden_cross"), buyBar.ObservationID, lease.FencingToken)
	if err != nil || buy == nil {
		t.Fatalf("later BUY admission failed: %v", err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-12T07:00:00Z") }
	lease = mustK2CLease(t, svc, k2aAccountRef)
	recordG38C2FillBar(t, svc, "later-buy-fill", "201", "100", "2026-01-12")
	buy, err = svc.RunPaperOrder(ctx, buy.OrderID, lease.FencingToken)
	if err != nil || buy == nil || buy.Status != "FILLED" {
		t.Fatalf("later BUY fill failed: %v", err)
	}
	before := paperAdmissionCountsForTest(t, svc)
	again, order, err := svc.AdmitPaperProposal(ctx, k2aAccountRef, signal.StrategySelectionEventID, raw, bar.ObservationID, lease.FencingToken)
	if err != nil || again == nil || again.EventID != event.EventID || order != nil || before != paperAdmissionCountsForTest(t, svc) {
		t.Fatalf("committed no-delta retry created a new command: %v", err)
	}
}

func hashPaperProposalForTest(t *testing.T, body map[string]any) []byte {
	t.Helper()
	delete(body, "proposal_sha256")
	raw, err := strategyCanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	body["proposal_sha256"] = hex.EncodeToString(hash[:])
	raw, err = strategyCanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPaperProposalAdmissionCreatesOneBoundOrder(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	raw := paperProposalForTest(t, svc, signal, "golden_cross")
	event, order, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID,
		raw, signal.SignalBarObservationID, lease.FencingToken)
	if err != nil || event == nil || order == nil || order.Status != "OPEN" || order.Quantity != "10" {
		t.Fatalf("proposal did not admit an OPEN paper order: event=%+v order=%+v err=%v", event, order, err)
	}
	before := paperAdmissionCountsForTest(t, svc)
	again, retry, err := svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID,
		raw, signal.SignalBarObservationID, lease.FencingToken)
	if err != nil || again.EventID != event.EventID || retry.OrderID != order.OrderID || before != paperAdmissionCountsForTest(t, svc) {
		t.Fatalf("proposal retry changed durable state: %v", err)
	}
	if _, err := provePaperAccountingRecovery(context.Background(), svc.db); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:31Z") }
	lease = mustK2CLease(t, svc, k2aAccountRef)
	recordG38C2FillBar(t, svc, "post-admission-new-bar", "101", "100", "2026-01-10")
	again, retry, err = svc.AdmitPaperProposal(context.Background(), k2aAccountRef, signal.StrategySelectionEventID, raw, signal.SignalBarObservationID, lease.FencingToken)
	if err != nil || again.EventID != event.EventID || retry.OrderID != order.OrderID || before != paperAdmissionCountsForTest(t, svc) {
		t.Fatalf("committed replay after deadline failed: %v", err)
	}
}
