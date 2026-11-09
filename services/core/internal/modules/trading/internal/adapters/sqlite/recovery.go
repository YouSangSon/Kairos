package sqlite

import (
	"context"
	"database/sql"
	"omni-folio/services/core/internal/modules/trading/contracts"
)

func (s *Store) VerifyBackup(ctx context.Context, query contracts.BackupQuery) (*contracts.RecoverySnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireSchemaVersionContext(ctx, tx, query.Schema); err != nil {
		return nil, err
	}
	result, err := verifyRecovery(ctx, tx, query)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func verifyRecovery(ctx context.Context, q orderQuerier, query contracts.BackupQuery) (*contracts.RecoverySnapshot, error) {
	result := &contracts.RecoverySnapshot{}
	orders, err := proveOrderRecovery(ctx, q)
	if err != nil {
		return nil, err
	}
	result.Orders = contracts.OrderRecovery(orders)
	strategy, err := proveStrategyRegistryRecovery(ctx, q)
	if err != nil {
		return nil, err
	}
	result.Strategy = contracts.StrategyRecovery{SHA256: strategy.SHA256, Evidence: strategy.Evidence, Events: strategy.Events, Evaluations: strategy.Evaluations, SelectedResultSHA256: strategy.SelectedResultSHA256, CurrentEventID: strategy.CurrentEventID}
	if query.Schema >= 15 {
		var accounting paperAccountingRecoveryProof
		if query.Archival && query.Schema == 15 {
			accounting, err = proveLegacyPaperAccountingRecovery(ctx, q)
		} else {
			accounting, err = provePaperAccountingRecovery(ctx, q)
		}
		if err != nil {
			return nil, err
		}
		result.Accounting = contracts.PaperAccountingRecovery(accounting)
	}
	if query.Schema >= 18 {
		proof, e := provePaperPerformanceRecovery(ctx, q)
		if e != nil {
			return nil, e
		}
		result.Performance = contracts.PaperPerformanceRecovery(proof)
	}
	if query.Schema >= 19 {
		proof, e := provePaperStrategyPerformanceRecovery(ctx, q)
		if e != nil {
			return nil, e
		}
		result.StrategyPerformance = contracts.PaperStrategyPerformanceRecovery(proof)
	}
	policy, err := provePaperPerformancePolicyRecovery(ctx, q)
	if err != nil {
		return nil, err
	}
	result.Policy = contracts.PaperPerformancePolicyRecovery(policy)
	if query.Schema >= 22 {
		proof, e := provePaperRunnerLeaseRecoveryAtSchema(ctx, q, query.Schema)
		if e != nil {
			return nil, e
		}
		result.Runner = contracts.RunnerLeaseRecovery(proof)
	}
	promotion, err := provePaperPromotionRecovery(ctx, q)
	if err != nil {
		return nil, err
	}
	result.Promotion = contracts.PromotionRecovery(promotion)
	return result, nil
}
