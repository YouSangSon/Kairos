package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/contracts"
)

func (s *Store) RegisterEvidence(ctx context.Context, raw []byte) (*contracts.StrategyEvidence, error) {
	record, err := s.registerStrategyEvidence(ctx, raw)
	if err != nil {
		return nil, err
	}
	result := record.StrategyEvidence
	return &result, nil
}
