package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

type PaperImprovementReceipt = contracts.PaperImprovementReceipt

func (s *Store) ImprovePaperCandidate(ctx context.Context, account, attempt string, artifact, raw []byte) (*PaperImprovementReceipt, error) {
	if s == nil || s.db == nil {
		if ctx == nil {
			return nil, errors.New("paper improvement context is required")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("paper improvement identifiers are invalid")
	}
	return s.business().ImprovePaperCandidate(ctx, account, attempt, artifact, raw)
}

func (s *Store) ImprovementContext(ctx context.Context, attempt string, work func(application.StrategyRegistryProof, *application.PromotionClaim, bool) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireSchemaContext(ctx, tx); err != nil {
		return err
	}
	registry, err := replayStrategyRegistry(ctx, tx)
	if err != nil {
		return err
	}
	claim, _, found, err := loadPaperPromotion(ctx, tx, attempt)
	if err != nil {
		return err
	}
	if err := work(registry, claim, found); err != nil {
		return err
	}
	return tx.Commit()
}
