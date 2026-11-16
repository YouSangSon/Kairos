package sqlite

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
)

type LocalPaperStepResult = contracts.LocalPaperStepResult

func (s *Store) ExecuteLocalPaper(ctx context.Context, account, selection string, proposalRaw, barsRaw, researchRaw []byte) (result *LocalPaperStepResult, resultErr error) {
	return s.business().ExecuteLocalPaper(ctx, account, selection, proposalRaw, barsRaw, researchRaw)
}

func (s *Store) localPaperPolicyCloses(ctx context.Context, account, latest string, committed bool) ([]string, error) {
	return s.business().LocalPaperPolicyCloses(ctx, account, latest, committed)
}

func (s *Store) openLocalPaperOrderIDs(ctx context.Context, account string) ([]string, error) {
	return s.business().OpenLocalPaperOrderIDs(ctx, account)
}

func (s *Store) startLocalPaperExecution(ctx context.Context, accountRef, selectionEventID string) (*ExecutionAuthorityState, error) {
	return s.startLocalPaperExecutionWithClaim(ctx, accountRef, selectionEventID, nil)
}

func (s *Store) startLocalPaperExecutionWithClaim(ctx context.Context, accountRef, selectionEventID string, claim *paperRunnerClaim) (*ExecutionAuthorityState, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("local paper execution is not configured")
	}
	return s.business().StartLocalPaperExecutionWithClaim(ctx, accountRef, selectionEventID, claim)
}

func (s *Store) haltOwnedSyntheticExecutionLease(ctx context.Context, accountRef string, fence int64) error {
	if s == nil || s.db == nil {
		return errors.New("local paper cleanup is not configured")
	}
	return s.business().HaltOwnedSyntheticExecutionLease(ctx, accountRef, fence)
}

func requireIsolatedPaperAccount(ctx context.Context, q orderQuerier, account string) error {
	var mixed bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_idempotency WHERE account_ref=? AND mode<>'paper')`, account).Scan(&mixed); err != nil {
		return err
	}
	if err := domain.ValidateIsolatedPaperAccount(mixed); err != nil {
		return err
	}
	return nil
}

func (s *Store) BeginLocalPaperRun(account, selection string) contracts.LocalPaperRun {
	return s.business().BeginLocalPaperRun(account, selection)
}
