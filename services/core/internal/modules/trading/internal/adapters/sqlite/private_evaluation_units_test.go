package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestG38PaperEvaluationDerivesInsufficientForLocallyAcknowledgedPaperOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("empty then active", func(t *testing.T) {
		svc, signal, _ := g38c2PaperSignalFixture(t, true)

		insufficient, err := svc.EvaluatePaperOperations(ctx, k2aAccountRef, signal.StrategyResultSHA256, signal.StrategySelectionEventID)
		if err != nil {
			t.Fatal(err)
		}
		if insufficient.Decision != "INSUFFICIENT" || insufficient.ReasonCode != "no_terminal_sample" ||
			insufficient.OrderCount != 0 || insufficient.TerminalOrderCount != 0 ||
			insufficient.ActiveOrderCount != 0 || insufficient.PendingActionCount != 0 {
			t.Fatalf("unexpected empty evaluation: %+v", insufficient)
		}

		signal.SignalID = "paper-evaluation-open"
		signal.TargetQuantity = "1"
		lease := mustK2CLease(t, svc, k2aAccountRef)
		_, state, err := svc.AdmitPaperSignal(ctx, k2aAccountRef, signal, lease.FencingToken)
		if err != nil || state == nil || state.Status != "OPEN" {
			t.Fatalf("paper authorization state=%+v err=%v", state, err)
		}

		active, err := svc.EvaluatePaperOperations(ctx, k2aAccountRef, signal.StrategyResultSHA256, signal.StrategySelectionEventID)
		if err != nil {
			t.Fatal(err)
		}
		if active.Decision != "INSUFFICIENT" || active.ReasonCode != "no_terminal_sample" ||
			active.OrderCount != 1 || active.TerminalOrderCount != 0 ||
			active.ActiveOrderCount != 1 || active.PendingActionCount != 0 ||
			active.ExpectedPreviousEvaluationID != insufficient.EvaluationID || active.PaperOrderStateSHA256 == "" {
			t.Fatalf("unexpected active evaluation: %+v", active)
		}
	})

	t.Run("unresolved policy remains degraded", func(t *testing.T) {
		decision, reason := paperOperationalDecision(paperOperationalSnapshot{Orders: 1, Pending: 1})
		if decision != "DEGRADED" || reason != "unresolved_action" {
			t.Fatalf("unexpected unresolved decision=%s reason=%s", decision, reason)
		}
	})
}

func TestG38PaperEvaluationIsIdempotentAndSelectionBound(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
	ctx := context.Background()
	firstEvidence, firstSelection := selectedPaperStrategy(t, svc)

	first, err := svc.EvaluatePaperOperations(ctx, k2aAccountRef, firstEvidence.ResultSHA256, firstSelection.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.EvaluatePaperOperations(ctx, k2aAccountRef, firstEvidence.ResultSHA256, firstSelection.CurrentEventID)
	if err != nil || replayed.EvaluationID != first.EvaluationID {
		t.Fatalf("idempotent evaluation=%+v err=%v", replayed, err)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM paper_evaluation_events`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("evaluation count=%d, want 1: %v", count, err)
	}

	secondEvidence, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
		config["experiment_id"] = "sma-grid-paper-evaluation-002"
		config["strategy"].(map[string]any)["version"] = "1.0.2"
	}))
	if err != nil {
		t.Fatal(err)
	}
	secondSelection, err := svc.SelectPaperCandidate(ctx, secondEvidence.ResultSHA256, firstSelection.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	var sealedEvaluationSequence int64
	if err := svc.db.QueryRow(`SELECT paper_evaluation_sequence FROM strategy_selection_events WHERE event_id=?`, secondSelection.CurrentEventID).
		Scan(&sealedEvaluationSequence); err != nil || sealedEvaluationSequence != 1 {
		t.Fatalf("selection sealed evaluation sequence=%d, want 1: %v", sealedEvaluationSequence, err)
	}
	if _, err := svc.EvaluatePaperOperations(ctx, k2aAccountRef, firstEvidence.ResultSHA256, firstSelection.CurrentEventID); err == nil {
		t.Fatal("stale paper selection was evaluated")
	}
	stale := *first
	stale.PaperOrderStateSHA256 = strings.Repeat("f", 64)
	stale.EvaluationID = paperEvaluationID(stale)
	stale.ExpectedPreviousEvaluationID = first.EvaluationID
	stale.RecordedAt = "2026-01-10T15:00:00Z"
	if err := insertPaperEvaluationDirect(svc, stale); err == nil {
		t.Fatal("SQLite accepted an evaluation for a superseded strategy selection")
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_evaluation_events_state_guard`); err != nil {
		t.Fatal(err)
	}
	if err := insertPaperEvaluationDirect(svc, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := proveStrategyRegistryRecovery(ctx, svc.db); err == nil {
		t.Fatal("strategy recovery certified a superseded paper evaluation")
	}
	if _, err := svc.EvaluatePaperOperations(ctx, "not-an-account", secondEvidence.ResultSHA256, secondSelection.CurrentEventID); err == nil {
		t.Fatal("invalid paper account was evaluated")
	}
}

func TestG38PaperEvaluationDoesNotMutateStrategyOrExecutionAuthority(t *testing.T) {
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	ctx := context.Background()
	lease := mustK2CLease(t, svc, k2aAccountRef)
	signal.SignalID, signal.TargetQuantity = "paper-evaluation-no-mutation", "1"
	if _, _, err := svc.AdmitPaperSignal(ctx, k2aAccountRef, signal, lease.FencingToken); err != nil {
		t.Fatal(err)
	}

	evaluation, err := svc.EvaluatePaperOperations(ctx, k2aAccountRef, signal.StrategyResultSHA256, signal.StrategySelectionEventID)
	if err != nil || evaluation.Decision != "INSUFFICIENT" || evaluation.ActiveOrderCount != 1 {
		t.Fatalf("evaluation=%+v err=%v", evaluation, err)
	}
	authority, err := loadExecutionAuthoritySnapshot(ctx, svc.db, k2aAccountRef)
	if err != nil || !authority.Armed || authority.FencingToken != lease.FencingToken || authority.LeaseOwner != lease.LeaseOwner {
		t.Fatalf("evaluation changed authority: %+v err=%v", authority, err)
	}
	registry, err := replayStrategyRegistry(ctx, svc.db)
	if err != nil || registry.CurrentEventID != signal.StrategySelectionEventID || registry.SelectedResultSHA256 != signal.StrategyResultSHA256 || registry.Events != 1 {
		t.Fatalf("evaluation changed strategy selection: %+v err=%v", registry, err)
	}
}

func TestG38PaperEvaluationIsInsertOnlyAndRecoveryVerified(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
	evidence, selected := selectedPaperStrategy(t, svc)
	evaluation, err := svc.EvaluatePaperOperations(context.Background(), k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE paper_evaluation_events SET decision='PASS'`,
		`DELETE FROM paper_evaluation_events`,
	} {
		if _, err := svc.db.Exec(statement); err == nil {
			t.Fatalf("insert-only evaluation log accepted mutation: %s", statement)
		}
	}

	stale := *evaluation
	stale.PaperOrderStateSHA256 = strings.Repeat("f", 64)
	stale.EvaluationID = paperEvaluationID(stale)
	stale.ExpectedPreviousEvaluationID = noPaperEvaluation
	stale.RecordedAt = "2026-01-10T15:00:01Z"
	if err := insertPaperEvaluationDirect(svc, stale); err == nil {
		t.Fatal("SQLite accepted a stale paper evaluation predecessor")
	}

	if _, err := svc.db.Exec(`DROP TRIGGER paper_evaluation_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE paper_evaluation_events SET record_sha256=? WHERE evaluation_id=?`, strings.Repeat("0", 64), evaluation.EvaluationID); err != nil {
		t.Fatal(err)
	}
	if _, err := proveStrategyRegistryRecovery(context.Background(), svc.db); err == nil {
		t.Fatal("strategy recovery certified a tampered paper evaluation")
	}
}

func insertPaperEvaluationDirect(svc *Store, event PaperEvaluationEvent) error {
	recordJSON, recordSHA, err := orderJSONHash(event)
	if err != nil {
		return err
	}
	_, err = svc.db.Exec(`INSERT INTO paper_evaluation_events(
		evaluation_id,schema_version,policy_version,account_ref,strategy_result_sha256,strategy_selection_event_id,
		expected_previous_evaluation_id,paper_order_state_sha256,order_count,terminal_order_count,active_order_count,
		pending_action_count,decision,reason_code,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.EvaluationID, event.SchemaVersion, event.PolicyVersion,
		event.AccountRef, event.StrategyResultSHA256, event.StrategySelectionEventID, event.ExpectedPreviousEvaluationID,
		event.PaperOrderStateSHA256, event.OrderCount, event.TerminalOrderCount, event.ActiveOrderCount,
		event.PendingActionCount, event.Decision, event.ReasonCode, recordSHA, string(recordJSON), event.RecordedAt)
	return err
}

func TestG38PaperEvaluationFailsClosedOnOrderCorruption(t *testing.T) {
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	lease := mustK2CLease(t, svc, k2aAccountRef)
	signal.SignalID, signal.TargetQuantity = "paper-evaluation-corrupt-order", "1"
	if _, _, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER order_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE order_events SET event_sha256=? WHERE event_type='SUBMIT_ACKNOWLEDGED'`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EvaluatePaperOperations(context.Background(), k2aAccountRef, signal.StrategyResultSHA256, signal.StrategySelectionEventID); err == nil {
		t.Fatal("paper evaluation accepted a corrupt order log")
	}
}

func paperEvaluationSignal(resultSHA, selectionEventID, signalID string) PaperSignal {
	return PaperSignal{
		SchemaVersion: paperSignalSchema, SignalID: signalID,
		StrategyResultSHA256: resultSHA, StrategySelectionEventID: selectionEventID,
		DataSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Symbol:     "005930", TargetQuantity: "1", DataAsOf: "2026-01-10T14:59:00Z",
		GeneratedAt: "2026-01-10T14:59:01Z", ExpiresAt: "2026-01-10T15:01:00Z",
	}
}
