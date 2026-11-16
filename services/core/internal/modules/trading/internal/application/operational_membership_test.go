package application

import (
	"context"
	"testing"
)

type excludedOperationalOrder struct{ stateReads int }

func (*excludedOperationalOrder) OperationalOrderIDs(context.Context, string) ([]string, error) {
	return []string{"excluded"}, nil
}
func (*excludedOperationalOrder) LoadOperationalIntent(context.Context, string) (OrderIntent, error) {
	return OrderIntent{StrategyResultSHA256: "other", StrategySelectionEventID: "selection"}, nil
}
func (r *excludedOperationalOrder) LoadOperationalState(context.Context, string) (*OrderState, error) {
	r.stateReads++
	return &OrderState{}, nil
}

func TestOperationalMembershipExclusionSkipsStateRead(t *testing.T) {
	reads := &excludedOperationalOrder{}
	snapshot, err := DerivePaperOperationalSnapshot(context.Background(), reads, "account", "result", "selection")
	if err != nil || snapshot.Orders != 0 || reads.stateReads != 0 {
		t.Fatalf("orders=%d stateReads=%d error=%v", snapshot.Orders, reads.stateReads, err)
	}
}
