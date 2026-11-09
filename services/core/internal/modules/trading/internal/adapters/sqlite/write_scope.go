package sqlite

import (
	"context"
	"database/sql"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"time"
)

type writeScope struct {
	store *Store
	tx    *sql.Tx
}

func (s *Store) Write(ctx context.Context, work func(application.WriteScope) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&writeScope{store: s, tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) business() *application.UseCases {
	return application.New(s, s.now, s.id, s.executionOwner, s.paperRunnerOwner)
}
func (j *writeScope) RecordPaperSignalEventTx(ctx context.Context, accountRef string, signal PaperSignal) (*PaperSignalEvent, error) {
	return j.store.recordPaperSignalEventTx(ctx, j.tx, accountRef, signal)
}

func (j *writeScope) PaperOrderBySignalFrom(ctx context.Context, accountRef, signalID string) (string, bool, error) {
	return paperOrderBySignalFrom(ctx, j.tx, accountRef, signalID)
}

func (j *writeScope) LoadOrderIntentFrom(ctx context.Context, orderID string) (OrderIntent, error) {
	return loadOrderIntentFrom(ctx, j.tx, orderID)
}

func (j *writeScope) ValidateCapitalizedPaperOrderBindings(ctx context.Context, intent OrderIntent) (*PaperAccountingSession, *PaperSignalEvent, error) {
	return validateCapitalizedPaperOrderBindings(ctx, j.tx, intent)
}

func (j *writeScope) LoadOrderStateFrom(ctx context.Context, orderID string) (*OrderState, error) {
	return loadOrderStateFrom(ctx, j.tx, orderID)
}

func (j *writeScope) ReplayPaperProjectionFrom(ctx context.Context, accountRef, symbol string) (application.Projection, error) {
	return replayPaperProjectionFrom(ctx, j.tx, accountRef, symbol)
}

func (j *writeScope) RecordOrderIntentTx(ctx context.Context, intent OrderIntent) (*OrderState, error) {
	return j.store.recordOrderIntentTx(ctx, j.tx, intent)
}

func (j *writeScope) LoadPaperExecutionAuthorizationByOrder(ctx context.Context, orderID string) (application.PaperAuthorization, bool, error) {
	return loadPaperExecutionAuthorizationByOrder(ctx, j.tx, orderID)
}

func (j *writeScope) ValidatePaperExecutionAuthorization(ctx context.Context, authorization application.PaperAuthorization) (*OrderState, error) {
	return validatePaperExecutionAuthorization(ctx, j.tx, authorization)
}

func (j *writeScope) AccountHasUnresolvedOrder(ctx context.Context, accountRef string) (bool, error) {
	return accountHasUnresolvedOrder(ctx, j.tx, accountRef)
}

func (j *writeScope) InsertPaperExecutionAuthorization(ctx context.Context, authorization application.PaperAuthorization) error {
	return insertPaperExecutionAuthorization(ctx, j.tx, authorization)
}

func (j *writeScope) AppendOrderEventTx(ctx context.Context, event OrderEvent, recordedAt string) (*OrderState, error) {
	return appendOrderEventTx(ctx, j.tx, event, recordedAt)
}

func (j *writeScope) ProvePaperAccountingRecovery(ctx context.Context) (application.PaperAccountingRecoveryProof, error) {
	return provePaperAccountingRecovery(ctx, j.tx)
}

func (j *writeScope) ValidatePaperRunnerLeaseTx(ctx context.Context, claim *application.RunnerClaim, accountRef, ownerID string, now time.Time) error {
	return validatePaperRunnerLeaseTx(ctx, j.tx, claim, accountRef, ownerID, now)
}

func (j *writeScope) ReplayPaperAccounting(ctx context.Context) (map[string]application.PaperAccountState, error) {
	return replayPaperAccounting(ctx, j.tx)
}

func (j *writeScope) PaperFillBars(ctx context.Context, signal PaperSignalEvent, delay int64) ([]*PaperMarketBarObservation, error) {
	return paperFillBars(ctx, j.tx, signal, delay)
}

func (j *writeScope) PaperOrderUsedFillBars(ctx context.Context, orderID string) (map[string]bool, error) {
	return paperOrderUsedFillBars(ctx, j.tx, orderID)
}

func (j *writeScope) PaperConsumedBarCapacity(ctx context.Context, accountRef, symbol, observationID string) (*big.Int, error) {
	return paperConsumedBarCapacity(ctx, j.tx, accountRef, symbol, observationID)
}

func (j *writeScope) AppendCapitalizedPaperFillTx(ctx context.Context, event OrderEvent, recordedAt string) (*OrderState, error) {
	return appendCapitalizedPaperFillTx(ctx, j.tx, event, recordedAt)
}

func (j *writeScope) ProvePaperRunnerLeaseRecovery(ctx context.Context) (application.PaperRunnerLeaseRecoveryProof, error) {
	return provePaperRunnerLeaseRecovery(ctx, j.tx)
}

func (j *writeScope) LoadPaperAccountingSession(ctx context.Context, accountRef string) (*PaperAccountingSession, bool, error) {
	return loadPaperAccountingSession(ctx, j.tx, accountRef)
}

func (j *writeScope) LoadCurrentStrategyExecutionPolicy(ctx context.Context, resultSHA256, eventID string) (application.StrategyExecutionPolicy, error) {
	return loadCurrentStrategyExecutionPolicy(ctx, j.tx, resultSHA256, eventID)
}

func (j *writeScope) RequireIsolatedPaperAccount(ctx context.Context, account string) error {
	return requireIsolatedPaperAccount(ctx, j.tx, account)
}

func (j *writeScope) LoadExecutionAuthorityRecordByID(ctx context.Context, eventID string) (application.AuthorityRecord, error) {
	return loadExecutionAuthorityRecordByID(ctx, j.tx, eventID)
}

func (j *writeScope) InsertExecutionAuthorityRecord(ctx context.Context, record application.AuthorityRecord) error {
	return insertExecutionAuthorityRecord(ctx, j.tx, record)
}

func (j *writeScope) RenewPaperRunnerLeaseTx(ctx context.Context, claim *application.RunnerClaim, ownerID string, now time.Time) (*application.RunnerClaim, error) {
	return renewPaperRunnerLeaseTx(ctx, j.tx, claim, ownerID, now)
}

func (j *writeScope) ProvePaperPerformancePolicyRecovery(ctx context.Context) (application.PaperPerformancePolicyRecoveryProof, error) {
	return provePaperPerformancePolicyRecovery(ctx, j.tx)
}

func (j *writeScope) LoadPaperPerformancePolicyByKey(ctx context.Context, accountRef, selectionEventID, strategyPerformanceID string) (*PaperPerformancePolicyEvent, bool, error) {
	return loadPaperPerformancePolicyByKey(ctx, j.tx, accountRef, selectionEventID, strategyPerformanceID)
}

func (j *writeScope) ReplayStrategyRegistry(ctx context.Context) (application.StrategyRegistryProof, error) {
	return replayStrategyRegistry(ctx, j.tx)
}

func (j *writeScope) LoadPaperStrategyPerformanceByID(ctx context.Context, id string) (PaperStrategyPerformanceEvent, int64, error) {
	return loadPaperStrategyPerformanceByID(ctx, j.tx, id)
}

func (j *writeScope) LatestPaperPerformancePolicyID(ctx context.Context, accountRef, selectionEventID string) (string, error) {
	return latestPaperPerformancePolicyID(ctx, j.tx, accountRef, selectionEventID)
}

func (j *writeScope) SelectionSequence(ctx context.Context) (int64, error) {
	return maxSequence(ctx, j.tx, "strategy_selection_events")
}
func (j *writeScope) AuthoritySequence(ctx context.Context) (int64, error) {
	return maxSequence(ctx, j.tx, "execution_authority_events")
}
func (j *writeScope) StrategyPerformanceSequence(ctx context.Context) (int64, error) {
	return maxSequence(ctx, j.tx, "paper_strategy_performance_events")
}
func (j *writeScope) ValidatePaperAccountingSession(session PaperAccountingSession) (application.StrategyExecutionPolicy, error) {
	return validatePaperAccountingSession(session)
}

func (j *writeScope) ArmedExecutionAccounts(ctx context.Context) ([]string, error) {
	return armedExecutionAccounts(ctx, j.tx)
}

func (j *writeScope) InsertPaperPerformancePolicyEvent(ctx context.Context, event PaperPerformancePolicyEvent) error {
	return insertPaperPerformancePolicyEvent(ctx, j.tx, event)
}

func (j *writeScope) RenewPaperRunnerLeaseAfterPolicyRollbackTx(ctx context.Context, claim *application.RunnerClaim, policyEventID, rollbackEventID, ownerID string, now time.Time) (*application.RunnerClaim, error) {
	return renewPaperRunnerLeaseAfterPolicyRollbackTx(ctx, j.tx, claim, policyEventID, rollbackEventID, ownerID, now)
}

func (j *writeScope) LoadRiskReservationByOrder(ctx context.Context, orderID string) (application.RiskReservation, bool, error) {
	return loadRiskReservationByOrder(ctx, j.tx, orderID)
}

func (j *writeScope) ValidateAuthorizedReservation(ctx context.Context, reservation application.RiskReservation) (*OrderState, error) {
	return validateAuthorizedReservation(ctx, j.tx, reservation)
}

func (j *writeScope) ValidateStrategyOrderSelection(ctx context.Context, intent OrderIntent) error {
	return validateStrategyOrderSelection(ctx, j.tx, intent)
}

func (j *writeScope) ActiveReservedNotional(ctx context.Context, accountRef string) (*big.Rat, error) {
	return activeReservedNotional(ctx, j.tx, accountRef)
}

func (j *writeScope) InsertRiskReservation(ctx context.Context, record application.RiskReservation) error {
	return insertRiskReservation(ctx, j.tx, record)
}

func (j *writeScope) LoadExecutionAuthoritySnapshot(ctx context.Context, accountRef string) (application.AuthoritySnapshot, error) {
	return loadExecutionAuthoritySnapshot(ctx, j.tx, accountRef)
}

func (j *writeScope) LatestPaperEvaluationSequence(ctx context.Context) (int64, error) {
	return latestPaperEvaluationSequence(ctx, j.tx)
}

func (j *writeScope) InsertStrategySelectionEvent(ctx context.Context, event StrategySelectionEvent) error {
	return insertStrategySelectionEvent(ctx, j.tx, event)
}
func (j *writeScope) EventSequence(ctx context.Context, id string) (int64, error) {
	var n int64
	err := j.tx.QueryRowContext(ctx, `SELECT sequence FROM order_events WHERE event_id=?`, id).Scan(&n)
	return n, err
}
func (j *writeScope) LatestStrategyPerformanceID(ctx context.Context, account string) (string, error) {
	var id string
	err := j.tx.QueryRowContext(ctx, `SELECT strategy_performance_id FROM paper_strategy_performance_events WHERE account_ref=? ORDER BY sequence DESC LIMIT 1`, account).Scan(&id)
	return id, err
}
func (j *writeScope) LatestAuthorityID(ctx context.Context, account string) (string, error) {
	var id string
	err := j.tx.QueryRowContext(ctx, `SELECT event_id FROM execution_authority_events WHERE account_ref=? ORDER BY sequence DESC LIMIT 1`, account).Scan(&id)
	return id, err
}
