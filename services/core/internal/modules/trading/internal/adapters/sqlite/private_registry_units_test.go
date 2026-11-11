package sqlite

import (
	"bytes"
	"context"
	"testing"
)

func TestG3RegistryRegistersPythonEvidenceAndSelectsFailClosed(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	ctx := context.Background()
	candidate := strategyArtifact(t, nil)

	evidence, err := svc.registerStrategyEvidence(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Target != "paper_candidate" || evidence.ResultSHA256 == "" {
		t.Fatalf("valid Python evidence was not admitted: %+v", evidence)
	}
	duplicate, err := svc.registerStrategyEvidence(ctx, candidate)
	if err != nil || duplicate.ResultSHA256 != evidence.ResultSHA256 {
		t.Fatalf("identical evidence was not idempotent: duplicate=%+v err=%v", duplicate, err)
	}
	var evidenceCount int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM strategy_research_evidence`).Scan(&evidenceCount); err != nil || evidenceCount != 1 {
		t.Fatalf("evidence count=%d, want 1: %v", evidenceCount, err)
	}
	tampered := bytes.Replace(candidate, []byte(`"paper_candidate"`), []byte(`"live"`), 1)
	if _, err := svc.registerStrategyEvidence(ctx, tampered); err == nil {
		t.Fatal("tampered promotion evidence was admitted")
	}

	blocked := strategyArtifact(t, func(config map[string]any) {
		config["experiment_id"] = "sma-grid-blocked-001"
		config["promotion"].(map[string]any)["minimum_holdout_after_cost_return"] = "999"
	})
	rejected, err := svc.registerStrategyEvidence(ctx, blocked)
	if err != nil || rejected.Target != "no_promotion" {
		t.Fatalf("rejected evidence was not preserved: evidence=%+v err=%v", rejected, err)
	}
	if _, err := svc.SelectPaperCandidate(ctx, rejected.ResultSHA256, noStrategySelectionEvent); err == nil {
		t.Fatal("no_promotion evidence became the selected paper candidate")
	}
	direct := StrategySelectionEvent{
		EventID: "strategy_selection_direct_rejected", EventType: "SELECT", CandidateResultSHA256: rejected.ResultSHA256,
		ExpectedCurrentEventID: noStrategySelectionEvent, PreviousSelectedResultSHA256: noStrategySelection,
		SelectedResultSHA256: rejected.ResultSHA256, ReasonCode: "manual_selection", RecordedAt: "2026-01-10T15:02:00Z",
	}
	directJSON, directSHA, err := orderJSONHash(direct)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO strategy_selection_events(
		event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,
		previous_selected_result_sha256,selected_result_sha256,reason_code,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, direct.EventID, direct.EventType, direct.CandidateResultSHA256,
		direct.ExpectedCurrentEventID, nil, direct.PreviousSelectedResultSHA256, direct.SelectedResultSHA256,
		direct.ReasonCode, directSHA, string(directJSON), direct.RecordedAt); err == nil {
		t.Fatal("SQLite guard allowed direct selection of no_promotion evidence")
	}

	selected, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	if selected.SelectedResultSHA256 != evidence.ResultSHA256 || selected.CurrentEventID == noStrategySelectionEvent {
		t.Fatalf("paper candidate was not selected: %+v", selected)
	}
	if _, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent); err == nil {
		t.Fatal("stale expected_current_event_id appended a selection")
	}
	var eventCount int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM strategy_selection_events`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("selection event count=%d, want 1: %v", eventCount, err)
	}
}

func TestG3RegistryMatchesPythonCanonicalJSONForHTMLCharacters(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	artifact := strategyArtifact(t, func(config map[string]any) {
		config["experiment_id"] = "sma-grid-<&>-canonical"
		config["strategy"].(map[string]any)["version"] = "1.0.0<&>"
	})
	if _, err := svc.registerStrategyEvidence(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}
}
