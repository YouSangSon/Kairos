package application

import (
	"context"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"time"
)

type RunnerLeaseRecord = domain.RunnerLeaseRecord

const paperRunnerLeaseScope = domain.PaperRunnerLeaseScope
const paperRunnerLeaseTTL = domain.PaperRunnerLeaseTTL

var ErrPaperRunnerLeaseHeld = domain.ErrPaperRunnerLeaseHeld
var errPaperRunnerLeaseHeld = ErrPaperRunnerLeaseHeld
var paperRunnerTimeNS = domain.PaperRunnerTimeNS
var paperRunnerExpiryNS = domain.PaperRunnerExpiryNS

func PaperRunnerClaimFromRecord(record RunnerLeaseRecord) *paperRunnerClaim {
	return &paperRunnerClaim{
		Scope: record.Scope, FencingToken: record.FencingToken, OwnerID: record.OwnerID, AccountRef: record.AccountRef,
		HeartbeatAtNS: record.HeartbeatAtNS, LeaseExpiresAtNS: record.ExpiresAtNS,
		StrategySelectionEventID: record.StrategySelectionEventID, SelectedResultSHA256: record.SelectedResultSHA256,
	}
}

var paperRunnerClaimFromRecord = PaperRunnerClaimFromRecord

func PaperRunnerRecordFromClaim(claim *paperRunnerClaim) RunnerLeaseRecord {
	if claim == nil {
		return RunnerLeaseRecord{}
	}
	return RunnerLeaseRecord{
		Scope: claim.Scope, FencingToken: claim.FencingToken, OwnerID: claim.OwnerID, AccountRef: claim.AccountRef,
		HeartbeatAtNS: claim.HeartbeatAtNS, ExpiresAtNS: claim.LeaseExpiresAtNS,
		StrategySelectionEventID: claim.StrategySelectionEventID, SelectedResultSHA256: claim.SelectedResultSHA256,
	}
}

var paperRunnerRecordFromClaim = PaperRunnerRecordFromClaim

func PaperRunnerClaimMatchesRecord(claim *paperRunnerClaim, record RunnerLeaseRecord) bool {
	return claim != nil && paperRunnerRecordFromClaim(claim) == record
}

var paperRunnerClaimMatchesRecord = PaperRunnerClaimMatchesRecord

func ClaimAccountRef(claim *paperRunnerClaim) string {
	if claim == nil {
		return ""
	}
	return claim.AccountRef
}

var claimAccountRef = ClaimAccountRef

func (s *UseCases) AcquirePaperRunnerLease(ctx context.Context, accountRef string) (*paperRunnerClaim, error) {
	if s == nil || s.journal == nil || s.now == nil || s.paperRunnerOwner == "" || !orderAlias(accountRef, "account") {
		return nil, errors.New("paper runner lease is not configured")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*paperRunnerClaim, error) {
		if _, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx); err != nil {
			return nil, fmt.Errorf("paper runner lease recovery: %w", err)
		}
		row, err := tx.Runner().LoadLease(ctx)
		if err != nil {
			return nil, err
		}
		nowNS, err := paperRunnerTimeNS(s.now())
		if err != nil {
			return nil, err
		}
		_, selectionEventID, selectedResult, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
		if bindErr := domain.ValidateRunnerSelectedStrategy(err == nil, selectedResult); bindErr != nil {
			return nil, errors.Join(bindErr, err)
		}
		next, changed, err := domain.AcquireRunnerLease(row, s.paperRunnerOwner, accountRef, selectionEventID, selectedResult, nowNS)
		if err != nil {
			return nil, err
		}
		if !changed {
			return paperRunnerClaimFromRecord(row), nil
		}
		if err := tx.Runner().CompareAndSwapLease(ctx, row, next); err != nil {
			return nil, err
		}
		return paperRunnerClaimFromRecord(next), nil
	})
}
func (s *UseCases) HeartbeatPaperRunnerLease(ctx context.Context, claim *paperRunnerClaim) (*paperRunnerClaim, error) {
	if s == nil || s.journal == nil || s.now == nil || claim == nil || claim.OwnerID != s.paperRunnerOwner {
		return nil, errors.New("paper runner lease is not configured")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*paperRunnerClaim, error) {
		if _, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx); err != nil {
			return nil, fmt.Errorf("paper runner lease recovery: %w", err)
		}
		next, err := RenewPaperRunnerLeaseTx(ctx, tx, claim, s.paperRunnerOwner, s.now())
		if err != nil {
			return nil, err
		}
		return next, nil
	})
}
func (s *UseCases) ReleasePaperRunnerLease(ctx context.Context, claim *paperRunnerClaim) error {
	if s == nil || s.journal == nil || claim == nil || claim.OwnerID != s.paperRunnerOwner {
		return errors.New("paper runner lease is not configured")
	}
	return s.journal.Write(ctx, func(tx WriteScope) error {
		if _, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx); err != nil {
			return fmt.Errorf("paper runner lease recovery: %w", err)
		}
		row, err := tx.Runner().LoadLease(ctx)
		if err != nil {
			return err
		}
		if !paperRunnerClaimMatchesRecord(claim, row) || row.OwnerID != s.paperRunnerOwner {
			return errors.New("paper runner lease was lost")
		}
		released := RunnerLeaseRecord{Scope: paperRunnerLeaseScope, FencingToken: row.FencingToken}
		if err := tx.Runner().CompareAndSwapLease(ctx, row, released); err != nil {
			return err
		}
		return nil
	})
}
func ValidatePaperRunnerLeaseTx(ctx context.Context, tx RunnerLeaseReader, claim *paperRunnerClaim, accountRef, ownerID string, now time.Time) error {
	row, err := tx.LoadLease(ctx)
	if err != nil {
		return err
	}
	claimRecord := paperRunnerRecordFromClaim(claim)
	if err := domain.ValidateRunnerClaimBinding(claim != nil, claimRecord, row, ownerID, accountRef); err != nil {
		return err
	}
	nowNS, err := paperRunnerTimeNS(now)
	if err != nil {
		return err
	}
	if err := domain.ValidateRunnerLeaseTime(row, nowNS); err != nil {
		return err
	}
	_, eventID, selectedResult, err := tx.CurrentPaperPerformanceSelection(ctx)
	if bindErr := domain.ValidateRunnerSelectionBinding(err == nil, claimRecord, eventID, selectedResult); bindErr != nil {
		return errors.Join(bindErr, err)
	}
	return nil
}
func RenewPaperRunnerLeaseTx(ctx context.Context, tx WriteScope, claim *paperRunnerClaim, ownerID string, now time.Time) (*paperRunnerClaim, error) {
	if err := ValidatePaperRunnerLeaseTx(ctx, tx.Runner(), claim, claimAccountRef(claim), ownerID, now); err != nil {
		return nil, err
	}
	return RenewPaperRunnerLeaseRowTx(ctx, tx, claim, now)
}
func RenewPaperRunnerLeaseAfterPolicyRollbackTx(ctx context.Context, tx WriteScope, claim *paperRunnerClaim, policyEventID, rollbackEventID, ownerID string, now time.Time) (*paperRunnerClaim, error) {
	row, err := tx.Runner().LoadLease(ctx)
	if err != nil {
		return nil, err
	}
	claimRecord := paperRunnerRecordFromClaim(claim)
	if err := domain.ValidateRunnerClaimBinding(claim != nil, claimRecord, row, ownerID, claimAccountRef(claim)); err != nil {
		return nil, err
	}
	nowNS, err := paperRunnerTimeNS(now)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateRunnerLeaseTime(row, nowNS); err != nil {
		return nil, err
	}
	facts, err := tx.Runner().RollbackBinding(ctx, rollbackEventID, policyEventID)
	if err != nil {
		return nil, err
	}
	_, currentEventID, _, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
	if bindErr := domain.ValidateRunnerRollbackBinding(err == nil, claimRecord, facts, currentEventID, rollbackEventID, policyEventID); bindErr != nil {
		return nil, errors.Join(bindErr, err)
	}
	return RenewPaperRunnerLeaseRowTx(ctx, tx, claim, now)
}
func RenewPaperRunnerLeaseRowTx(ctx context.Context, tx WriteScope, claim *paperRunnerClaim, now time.Time) (*paperRunnerClaim, error) {
	nowNS, err := paperRunnerTimeNS(now)
	if err != nil {
		return nil, err
	}
	expiresNS, err := paperRunnerExpiryNS(nowNS)
	if err != nil {
		return nil, err
	}
	previous := paperRunnerRecordFromClaim(claim)
	next := previous
	next.HeartbeatAtNS, next.ExpiresAtNS = nowNS, expiresNS
	if err := tx.Runner().CompareAndSwapLease(ctx, previous, next); err != nil {
		return nil, err
	}
	return paperRunnerClaimFromRecord(next), nil
}
