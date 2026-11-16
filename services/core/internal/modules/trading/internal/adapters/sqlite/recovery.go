package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
)

func (s *Store) VerifyRecovery(ctx context.Context) (*contracts.RecoverySnapshot, error) {
	query := contracts.BackupQuery{Schema: latestSchema}
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

func (s *Store) VerifyBackup(ctx context.Context, ref storage.ObservationRef, query contracts.BackupQuery) (*contracts.RecoverySnapshot, error) {
	if s.backupReader == nil {
		return nil, observation.ErrReference
	}
	handle, err := s.backupReader.Resolve(ref)
	if err != nil {
		return nil, err
	}
	q := observedQueries{handle}
	if err := requireSchemaVersionContext(ctx, q, query.Schema); err != nil {
		return nil, err
	}
	return verifyRecovery(ctx, q, query)
}
func (s *Store) MigrationFacts(ctx context.Context, ref storage.ObservationRef, query contracts.MigrationQuery) (*contracts.MigrationSnapshot, error) {
	if s.migrationReader == nil {
		return nil, observation.ErrReference
	}
	handle, err := s.migrationReader.Resolve(ref)
	if err != nil {
		return nil, err
	}
	q := observedQueries{handle}
	if query.Schema != 19 && query.Schema != 26 {
		return nil, errors.New("unsupported trading migration proof schema")
	}
	if query.Schema == 19 {
		var version int
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
			return nil, err
		}
		if version != 19 {
			return nil, fmt.Errorf("expected schema version 19, got %d", version)
		}
	} else if err := requireSchemaVersionContext(ctx, q, 26); err != nil {
		return nil, err
	}
	result := &contracts.MigrationSnapshot{}
	strategy, err := proveStrategyRegistryRecovery(ctx, q)
	if err != nil {
		return nil, err
	}
	result.Strategy = contracts.StrategyRecovery{SHA256: strategy.SHA256, Evidence: strategy.Evidence, Events: strategy.Events, Evaluations: strategy.Evaluations, SelectedResultSHA256: strategy.SelectedResultSHA256, CurrentEventID: strategy.CurrentEventID}
	if query.Schema == 19 {
		result.AuthoritySHA, result.AuthorityCount, err = proveExecutionAuthorityRecovery(ctx, q)
		if err != nil {
			return nil, err
		}
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(sequence), 0) FROM execution_authority_events`).Scan(&result.AuthorityShape.Rows, &result.AuthorityShape.MaxSequence); err != nil {
			return nil, err
		}
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(sequence), 0) FROM strategy_selection_events`).Scan(&result.StrategyShape.Rows, &result.StrategyShape.MaxSequence); err != nil {
			return nil, err
		}
	} else {
		orders, err := proveOrderRecovery(ctx, q)
		if err != nil {
			return nil, err
		}
		result.Orders = contracts.OrderRecovery(orders)
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(sequence), 0) FROM strategy_selection_events`).Scan(&result.SelectionShape.Rows, &result.SelectionShape.MaxSequence); err != nil {
			return nil, err
		}
		if query.PostRebuild {
			if err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM paper_promotion_claims) +
		(SELECT COUNT(*) FROM paper_promotion_decisions) + (SELECT COUNT(*) FROM strategy_selection_events WHERE paper_promotion_attempt_id IS NOT NULL)`).Scan(&result.AddedPromotionRows); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

type observedQueries struct{ handle *observation.ReadHandle }

func (q observedQueries) QueryContext(ctx context.Context, query string, args ...any) (rowset, error) {
	return q.handle.QueryContext(ctx, query, args...)
}
func (q observedQueries) QueryRowContext(ctx context.Context, query string, args ...any) rowScanner {
	return q.handle.QueryOne(ctx, query, args...)
}

func (s *Store) VerifyStartup(ctx context.Context, ref storage.ObservationRef) error {
	if s.backupReader == nil {
		return observation.ErrReference
	}
	handle, err := s.backupReader.Resolve(ref)
	if err != nil {
		return err
	}
	q := observedQueries{handle}
	if _, err := provePaperPerformancePolicyRecovery(ctx, q); err != nil {
		return fmt.Errorf("paper performance policy startup recovery: %w", err)
	}
	if _, err := provePaperRunnerLeaseRecovery(ctx, q); err != nil {
		return fmt.Errorf("paper runner lease startup recovery: %w", err)
	}
	if _, err := provePaperPromotionRecovery(ctx, q); err != nil {
		return fmt.Errorf("paper promotion startup recovery: %w", err)
	}
	return nil
}
