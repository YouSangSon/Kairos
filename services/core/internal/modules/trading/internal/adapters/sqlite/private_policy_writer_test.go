package sqlite

import (
	"context"
	"testing"
)

func TestG38ESchema20PublicLinkedJournalWritersRejectTamperedPolicy(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_performance_policy_events_no_update;
		UPDATE paper_performance_policy_events SET record_sha256=? WHERE policy_event_id=?`, emptySHA256, event.PolicyEventID); err != nil {
		t.Fatal(err)
	}
	before := g38EJournalCounts(t, svc)
	var evidenceBefore int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM strategy_research_evidence`).Scan(&evidenceBefore); err != nil {
		t.Fatal(err)
	}
	artifact := rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) { result["experiment_id"] = "g38e-root-writer" })
	if _, err := svc.registerStrategyEvidence(ctx, artifact); err == nil {
		t.Fatal("schema20 strategy evidence writer accepted a corrupt linked policy")
	}
	if _, err := svc.SetSyntheticExecutionArmed(ctx, k2aAccountRef, true); err == nil {
		t.Fatal("schema20 authority arm accepted a corrupt linked policy")
	}
	if _, err := svc.AcquireSyntheticExecutionLease(ctx, k2aAccountRef); err == nil {
		t.Fatal("schema20 authority lease accepted a corrupt linked policy")
	}
	var evidenceAfter int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM strategy_research_evidence`).Scan(&evidenceAfter); err != nil {
		t.Fatal(err)
	}
	if after := g38EJournalCounts(t, svc); after != before || evidenceAfter != evidenceBefore {
		t.Fatalf("corrupt public writer appended rows journal=%+v/%+v evidence=%d/%d", before, after, evidenceBefore, evidenceAfter)
	}
}
