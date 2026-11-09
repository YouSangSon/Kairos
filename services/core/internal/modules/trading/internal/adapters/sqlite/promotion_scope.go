package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/researchdomain"
	"time"
)

func (j *writeScope) RequireSchema(ctx context.Context) error { return requireSchemaContext(ctx, j.tx) }
func (j *writeScope) ProvePaperPromotionRecovery(ctx context.Context) (contracts.PromotionRecovery, error) {
	p, e := provePaperPromotionRecovery(ctx, j.tx)
	return contracts.PromotionRecovery(p), e
}
func (j *writeScope) LoadPaperPromotion(ctx context.Context, id string) (*application.PromotionClaim, *application.PromotionDecision, bool, error) {
	return loadPaperPromotion(ctx, j.tx, id)
}
func (j *writeScope) PendingPromotionCount(ctx context.Context) (int, error) {
	var n int
	e := j.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM paper_promotion_claims c WHERE NOT EXISTS(SELECT 1 FROM paper_promotion_decisions d WHERE d.attempt_id=c.attempt_id)`).Scan(&n)
	return n, e
}
func (j *writeScope) PromotionInputs(ctx context.Context, candidate, incumbent, cash, policy string) (researchdomain.Strategy, researchdomain.Strategy, researchdomain.Execution, error) {
	return paperPromotionInputs(ctx, j.tx, candidate, incumbent, cash, policy)
}
func (s *Store) PromotionInputs(ctx context.Context, candidate, incumbent, cash, policy string) (researchdomain.Strategy, researchdomain.Strategy, researchdomain.Execution, error) {
	return paperPromotionInputs(ctx, s.db, candidate, incumbent, cash, policy)
}
func (s *Store) LoadPromotion(ctx context.Context, id string) (*application.PromotionClaim, *application.PromotionDecision, bool, error) {
	return loadPaperPromotion(ctx, s.db, id)
}
func (j *writeScope) OrderSequence(ctx context.Context) (int64, error) {
	var sequence int64
	err := j.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM order_events`).Scan(&sequence)
	return sequence, err
}
func (j *writeScope) ResearchSequence(ctx context.Context) (int64, error) {
	var sequence int64
	err := j.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM strategy_research_evidence`).Scan(&sequence)
	return sequence, err
}
func (j *writeScope) PreviousPromotionAttempt(ctx context.Context) (string, error) {
	var id string
	e := j.tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT attempt_id FROM paper_promotion_claims ORDER BY sequence DESC LIMIT 1),'no_attempt')`).Scan(&id)
	return id, e
}
func (j *writeScope) PromotionDecisionTime(ctx context.Context, id string) (string, error) {
	var at string
	e := j.tx.QueryRowContext(ctx, `SELECT recorded_at FROM paper_promotion_decisions WHERE attempt_id=?`, id).Scan(&at)
	return at, e
}
func (j *writeScope) ValidatePromotionClaim(ctx context.Context, claim application.PromotionClaim, sequence int64) error {
	return validateStoredPaperPromotionClaim(ctx, j.tx, claim, sequence)
}
func (j *writeScope) InsertPromotionClaim(ctx context.Context, claim application.PromotionClaim) error {
	canonical, sha, err := orderJSONHash(claim)
	if err != nil {
		return err
	}
	_, err = j.tx.ExecContext(ctx, `INSERT INTO paper_promotion_claims(attempt_id,record_sha256,record_json,recorded_at) VALUES(?,?,?,?)`, claim.AttemptID, sha, string(canonical), claim.RecordedAt)
	return err
}
func (j *writeScope) InsertPromotionDecision(ctx context.Context, decision application.PromotionDecision) error {
	canonical, sha, err := orderJSONHash(decision)
	if err != nil {
		return err
	}
	_, err = j.tx.ExecContext(ctx, `INSERT INTO paper_promotion_decisions(attempt_id,selection_event_id,record_sha256,record_json,recorded_at) VALUES(?,?,?,?,?)`, decision.AttemptID, nullable(decision.SelectionEventID), sha, string(canonical), decision.RecordedAt)
	return err
}

func (j *writeScope) SessionCount(ctx context.Context) (int, error) {
	var n int
	err := j.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM paper_accounting_sessions`).Scan(&n)
	return n, err
}
func (j *writeScope) ReplayAccountsWithoutRegistry(ctx context.Context) (map[string]application.PaperAccountState, error) {
	return replayPaperAccountingWithoutRegistry(ctx, j.tx, nil)
}
func (j *writeScope) RejectLiveLease(ctx context.Context, now time.Time) error {
	return rejectLivePaperRunnerLease(ctx, j.tx, now)
}
func (j *writeScope) RejectIncompleteRun(ctx context.Context, account, selection string) error {
	return rejectIncompletePaperRunForExpiredClaim(ctx, j.tx, paperRunnerLeaseRecord{AccountRef: account, StrategySelectionEventID: selection})
}
func (j *writeScope) ProveOrderRecovery(ctx context.Context) (contracts.OrderRecovery, error) {
	p, e := proveOrderRecovery(ctx, j.tx)
	return contracts.OrderRecovery(p), e
}
func (j *writeScope) ProveAuthorityRecovery(ctx context.Context) (string, int, error) {
	return proveExecutionAuthorityRecovery(ctx, j.tx)
}
func (j *writeScope) AccountOrderIDs(ctx context.Context, account string) ([]string, error) {
	rows, err := j.tx.QueryContext(ctx, `SELECT order_id FROM order_idempotency WHERE account_ref=?`, account)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return ids, nil
}
