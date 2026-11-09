package sqlite

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestG38C1PaperAccountingSessionDerivesImmutableCapital(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
	ctx := context.Background()
	evidence, selected := selectedPaperStrategy(t, svc)

	policy, err := loadCurrentStrategyExecutionPolicy(ctx, svc.db, evidence.ResultSHA256, selected.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	if session.SchemaVersion != "paper-accounting-session.v1" || session.AccountRef != k2aAccountRef ||
		session.StrategyResultSHA256 != evidence.ResultSHA256 || session.StrategySelectionEventID != selected.CurrentEventID ||
		session.StartingCash != "10000" || session.Currency != "KRW" || session.ExecutionPolicySHA256 != policy.SHA256 ||
		session.ExecutionPolicyJSON != policy.CanonicalJSON {
		t.Fatalf("derived session=%+v policy=%+v", session, policy)
	}
	replayed, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
	if err != nil || !reflect.DeepEqual(replayed, session) {
		t.Fatalf("exact idempotency session=%+v replay=%+v err=%v", session, replayed, err)
	}
	if count := paperAccountingSessionCount(t, svc); count != 1 {
		t.Fatalf("session count=%d, want 1", count)
	}

	second, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
		config["experiment_id"] = "paper-accounting-session-second"
		config["strategy"].(map[string]any)["version"] = "1.0.3"
	}))
	if err != nil {
		t.Fatal(err)
	}
	secondSelection, err := svc.SelectPaperCandidate(ctx, second.ResultSHA256, selected.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err = svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
	if err != nil || !reflect.DeepEqual(replayed, session) {
		t.Fatalf("initial session stopped being idempotent after strategy change: session=%+v replay=%+v err=%v", session, replayed, err)
	}
	if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, second.ResultSHA256, secondSelection.CurrentEventID); err == nil {
		t.Fatal("different initial strategy attempt reset account capital")
	}
}

func TestG38C1PaperAccountingSessionDecimalContract(t *testing.T) {
	t.Run("canonical fractional starting cash opens and stores exactly", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		artifact := rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) {
			result["execution"].(map[string]any)["starting_cash"] = "0.01"
		})
		evidence, err := svc.registerStrategyEvidence(ctx, artifact)
		if err != nil {
			t.Fatal(err)
		}
		selected, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent)
		if err != nil {
			t.Fatal(err)
		}
		session, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
		if err != nil {
			t.Fatal(err)
		}
		var stored string
		if err := svc.db.QueryRow(`SELECT starting_cash FROM paper_accounting_sessions WHERE session_id=?`, session.SessionID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if session.StartingCash != "0.01" || stored != "0.01" {
			t.Fatalf("fractional starting cash session=%q stored=%q", session.StartingCash, stored)
		}
	})

	t.Run("table rejects noncanonical fractional starting cash", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		evidence, selected := selectedPaperStrategy(t, svc)
		session := directPaperAccountingSession(t, svc, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
		session.StartingCash = "1.0"
		err := insertPaperAccountingSessionDirect(svc, session)
		if err == nil {
			t.Fatal("paper accounting table accepted noncanonical starting_cash 1.0")
		} else if !strings.Contains(err.Error(), "starting_cash") {
			t.Fatalf("noncanonical starting_cash 1.0 failed outside the table predicate: %v", err)
		}
	})
}

func TestG38C1PaperAccountingSessionConcurrentExactOpenCreatesOneRow(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
	ctx := context.Background()
	evidence, selected := selectedPaperStrategy(t, svc)

	const callers = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	sessions := make([]*PaperAccountingSession, callers)
	errs := make([]error, callers)
	for i := range sessions {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			sessions[index], errs[index] = svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
		}(i)
	}
	close(start)
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent open %d: %v", index, err)
		}
	}
	if sessions[0] == nil || sessions[1] == nil || sessions[0].SessionID != sessions[1].SessionID || paperAccountingSessionCount(t, svc) != 1 {
		t.Fatalf("concurrent sessions=%+v count=%d", sessions, paperAccountingSessionCount(t, svc))
	}
}

func paperAccountingSessionCount(t testing.TB, svc *Store) int {
	t.Helper()
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM paper_accounting_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func directPaperAccountingSession(t testing.TB, svc *Store, accountRef, resultSHA256, selectionEventID string) PaperAccountingSession {
	t.Helper()
	policy, err := loadCurrentStrategyExecutionPolicy(context.Background(), svc.db, resultSHA256, selectionEventID)
	if err != nil {
		t.Fatal(err)
	}
	session := PaperAccountingSession{
		SchemaVersion: "paper-accounting-session.v1", AccountRef: accountRef,
		StrategyResultSHA256: resultSHA256, StrategySelectionEventID: selectionEventID,
		ExecutionPolicySHA256: policy.SHA256, ExecutionPolicyJSON: policy.CanonicalJSON,
		StartingCash: policy.StartingCash, Currency: "KRW", RecordedAt: svc.now().UTC().Format(time.RFC3339Nano),
	}
	session.SessionID = paperAccountingSessionID(session.AccountRef, session.StrategyResultSHA256, session.StrategySelectionEventID, session.ExecutionPolicySHA256)
	return session
}

func insertPaperAccountingSessionDirect(svc *Store, session PaperAccountingSession) error {
	recordJSON, recordSHA, err := orderJSONHash(session)
	if err != nil {
		return err
	}
	_, err = svc.db.Exec(`INSERT INTO paper_accounting_sessions(
		session_id,schema_version,account_ref,strategy_result_sha256,strategy_selection_event_id,
		execution_policy_sha256,execution_policy_json,starting_cash,currency,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, session.SessionID, session.SchemaVersion, session.AccountRef,
		session.StrategyResultSHA256, session.StrategySelectionEventID, session.ExecutionPolicySHA256,
		session.ExecutionPolicyJSON, session.StartingCash, session.Currency, recordSHA, string(recordJSON), session.RecordedAt)
	return err
}
