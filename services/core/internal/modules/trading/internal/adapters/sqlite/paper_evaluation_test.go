package sqlite

import (
	"context"
	"testing"
)

func selectedPaperStrategy(t *testing.T, svc *Store) (*StrategyEvidence, *StrategySelectionState) {
	t.Helper()
	evidence, err := svc.registerStrategyEvidence(context.Background(), strategyArtifact(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(context.Background(), evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	return evidence, selected
}
