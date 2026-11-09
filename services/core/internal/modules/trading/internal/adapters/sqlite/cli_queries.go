package sqlite

import (
	"context"
)

func (s *Store) StrategyStatus(ctx context.Context) (*StrategySelectionState, error) {
	proof, err := proveStrategyRegistryRecovery(ctx, s.db)
	if err != nil {
		return nil, err
	}
	return &StrategySelectionState{CurrentEventID: proof.CurrentEventID, SelectedResultSHA256: proof.SelectedResultSHA256}, nil
}
func (s *Store) InitializePaperAccount(ctx context.Context, account, result, selection string) (*PaperAccountingSession, error) {
	return s.openPaperAccountingSessionChecked(ctx, account, result, selection, true)
}
