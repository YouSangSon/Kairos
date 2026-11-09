package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omni-folio/services/core/internal/modules/trading/internal/application"
)

func (j *writeScope) FindIntentKey(ctx context.Context, intent OrderIntent) (string, string, bool, error) {
	var sha, id string
	err := j.tx.QueryRowContext(ctx, `SELECT request_sha256, order_id FROM order_idempotency WHERE provider=? AND mode=? AND account_ref=? AND client_order_id=?`, intent.Provider, intent.Mode, intent.AccountRef, intent.ClientOrderID).Scan(&sha, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return sha, id, err == nil, err
}
func (j *writeScope) RecordIntent(ctx context.Context, intent OrderIntent, id, at string) error {
	raw, sha, err := orderJSONHash(intent)
	if err != nil {
		return err
	}
	_, err = j.tx.ExecContext(ctx, `INSERT INTO order_idempotency(provider,mode,account_ref,client_order_id,request_sha256,order_id,intent_json,recorded_at) VALUES(?,?,?,?,?,?,?,?)`, intent.Provider, intent.Mode, intent.AccountRef, intent.ClientOrderID, sha, id, string(raw), at)
	return err
}
func (j *writeScope) InsertOrderEvent(ctx context.Context, event OrderEvent, at string) error {
	return insertOrderEvent(ctx, j.tx, event, at)
}
func (j *writeScope) SchemaVersion(ctx context.Context) (int, error) {
	var n int
	err := j.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&n)
	return n, err
}
func (j *writeScope) FindEvidence(ctx context.Context, sha string) (string, string, bool, error) {
	var raw, at string
	err := j.tx.QueryRowContext(ctx, `SELECT artifact_json,recorded_at FROM strategy_research_evidence WHERE result_sha256=?`, sha).Scan(&raw, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return raw, at, err == nil, err
}
func (j *writeScope) RecordEvidence(ctx context.Context, e application.Evidence) error {
	_, err := j.tx.ExecContext(ctx, `INSERT INTO strategy_research_evidence(
		result_sha256,artifact_sha256,strategy_name,strategy_version,parameter_sha256,target,artifact_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?)`, e.ResultSHA256, e.ArtifactSHA256, e.StrategyName, e.StrategyVersion, e.ParameterSHA256, e.Target, e.ArtifactJSON, e.RecordedAt)
	return err
}
func (j *writeScope) EvidenceTarget(ctx context.Context, sha string) (string, bool, error) {
	var target string
	err := j.tx.QueryRowContext(ctx, `SELECT target FROM strategy_research_evidence WHERE result_sha256=?`, sha).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return target, err == nil, err
}
func (j *writeScope) RequirePromotionAppendTime(ctx context.Context, at string) error {
	return requirePaperPromotionAppendTime(ctx, j.tx, at)
}
func (j *writeScope) PaperOrderCount(ctx context.Context, account string) (int, error) {
	var n int
	err := j.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_idempotency WHERE mode='paper' AND account_ref=?`, account).Scan(&n)
	return n, err
}
func (j *writeScope) InsertAccountingSession(ctx context.Context, session PaperAccountingSession) error {
	return insertPaperAccountingSession(ctx, j.tx, session)
}
