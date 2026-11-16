package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"strings"
	"time"
)

const (
	// ponytail: global serialization remains until strategy selection itself is account-scoped; use per-account leases only then.
	paperRunnerLeaseScope = "paper_strategy_selection"
	paperRunnerLeaseTTL   = 30 * time.Second
)

var errPaperRunnerLeaseHeld = application.ErrPaperRunnerLeaseHeld

type paperRunnerLeaseRecord = domain.RunnerLeaseRecord

type paperRunnerClaim = application.RunnerClaim

type paperRunnerLeaseRecoveryProof = application.PaperRunnerLeaseRecoveryProof

type storedPaperRunnerLease struct {
	record     paperRunnerLeaseRecord
	recordSHA  string
	recordJSON string
}

func (s *Store) acquirePaperRunnerLease(ctx context.Context, accountRef string) (*paperRunnerClaim, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper runner lease is not configured")
	}
	return s.business().AcquirePaperRunnerLease(ctx, accountRef)
}

func (s *Store) heartbeatPaperRunnerLease(ctx context.Context, claim *paperRunnerClaim) (*paperRunnerClaim, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper runner lease is not configured")
	}
	return s.business().HeartbeatPaperRunnerLease(ctx, claim)
}

func (s *Store) releasePaperRunnerLease(ctx context.Context, claim *paperRunnerClaim) error {
	if s == nil || s.db == nil {
		return errors.New("paper runner lease is not configured")
	}
	return s.business().ReleasePaperRunnerLease(ctx, claim)
}

func validatePaperRunnerLeaseTx(ctx context.Context, q orderQuerier, claim *paperRunnerClaim, accountRef, ownerID string, now time.Time) error {
	return application.ValidatePaperRunnerLeaseTx(ctx, &runnerLeaseScope{q: q}, claim, accountRef, ownerID, now)
}

func renewPaperRunnerLeaseTx(ctx context.Context, tx *transaction, claim *paperRunnerClaim, ownerID string, now time.Time) (*paperRunnerClaim, error) {
	return application.RenewPaperRunnerLeaseTx(ctx, &writeScope{tx: tx}, claim, ownerID, now)
}

func renewPaperRunnerLeaseAfterPolicyRollbackTx(ctx context.Context, tx *transaction, claim *paperRunnerClaim, policyEventID, rollbackEventID, ownerID string, now time.Time) (*paperRunnerClaim, error) {
	return application.RenewPaperRunnerLeaseAfterPolicyRollbackTx(ctx, &writeScope{tx: tx}, claim, policyEventID, rollbackEventID, ownerID, now)
}

func renewPaperRunnerLeaseRowTx(ctx context.Context, tx *transaction, claim *paperRunnerClaim, now time.Time) (*paperRunnerClaim, error) {
	return application.RenewPaperRunnerLeaseRowTx(ctx, &writeScope{tx: tx}, claim, now)
}

func rejectLivePaperRunnerLease(ctx context.Context, q orderQuerier, now time.Time) error {
	if _, err := provePaperRunnerLeaseRecovery(ctx, q); err != nil {
		return fmt.Errorf("paper runner lease recovery: %w", err)
	}
	row, err := loadPaperRunnerLease(ctx, q)
	if err != nil {
		return err
	}
	if row.record.OwnerID == "" {
		return nil
	}
	nowNS, err := paperRunnerTimeNS(now)
	if err != nil {
		return err
	}
	if nowNS < row.record.HeartbeatAtNS {
		return errors.New("paper runner lease clock regressed")
	}
	if nowNS < row.record.ExpiresAtNS {
		return errors.New("paper runner is active; stop and release it before changing strategy selection")
	}
	if err := rejectIncompletePaperRunForExpiredClaim(ctx, q, row.record); err != nil {
		return err
	}
	return nil
}

func rejectIncompletePaperRunForExpiredClaim(ctx context.Context, q orderQuerier, lease paperRunnerLeaseRecord) error {
	var incomplete int
	err := q.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM paper_performance_events p
		WHERE p.account_ref=? AND p.strategy_selection_event_id=?
		  AND NOT EXISTS(
			SELECT 1 FROM paper_strategy_performance_events w
			JOIN paper_performance_policy_events e
			  ON e.account_ref=w.account_ref
			 AND e.strategy_selection_event_id=w.strategy_selection_event_id
			 AND e.strategy_performance_id=w.strategy_performance_id
			WHERE w.account_ref=p.account_ref
			  AND w.strategy_selection_event_id=p.strategy_selection_event_id
			  AND w.latest_performance_id=p.performance_id
		  )
	)`, lease.AccountRef, lease.StrategySelectionEventID).Scan(&incomplete)
	if err != nil {
		return err
	}
	if incomplete != 0 {
		return errors.New("expired paper runner has an incomplete paper performance chain")
	}
	return nil
}

func provePaperRunnerLeaseRecovery(ctx context.Context, q orderQuerier) (paperRunnerLeaseRecoveryProof, error) {
	return provePaperRunnerLeaseRecoveryAtSchema(ctx, q, latestSchema)
}

func provePaperRunnerLeaseRecoveryAtSchema(ctx context.Context, q orderQuerier, schema int) (paperRunnerLeaseRecoveryProof, error) {
	// Policy recovery has legacy early returns. Require current migration
	// history, or the exact v16 through v19 source history already declared by restore.
	if schema != latestSchema && schema != 22 && schema != 23 && schema != 24 && schema != 25 && schema != 26 {
		return paperRunnerLeaseRecoveryProof{}, errors.New("unsupported runner lease recovery schema")
	}
	if err := requireSchemaVersionContext(ctx, q, schema); err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	if _, err := provePaperPerformancePolicyRecovery(ctx, q); err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	if err := validatePaperRunnerLeaseSchema(ctx, q); err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	var rows, active int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER (WHERE owner_id IS NOT NULL) FROM paper_runner_leases`).Scan(&rows, &active); err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	if rows != 1 {
		return paperRunnerLeaseRecoveryProof{}, errors.New("paper runner lease singleton is invalid")
	}
	row, err := loadPaperRunnerLease(ctx, q)
	if err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	hash := sha256.New()
	if err := json.NewEncoder(hash).Encode([]any{"paper_runner_leases", row.record}); err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	return paperRunnerLeaseRecoveryProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Leases: rows, Active: active}, nil
}

func validatePaperRunnerLeaseSchema(ctx context.Context, q orderQuerier) error {
	migration, err := migrationFiles.ReadFile("migrations/021_paper_runner_leases.sql")
	if err != nil {
		return err
	}
	script := string(migration)
	const bindingIndexSQL = "create unique index strategy_selection_events_runner_binding_idx on strategy_selection_events(event_id, selected_result_sha256)"
	var bindingIndex string
	if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='index' AND name='strategy_selection_events_runner_binding_idx' AND tbl_name='strategy_selection_events'`).Scan(&bindingIndex); err != nil || normalizePaperRunnerLeaseSQL(bindingIndex) != bindingIndexSQL {
		return errors.Join(errors.New("paper runner lease selection-binding index does not match migration 021"), err)
	}
	const tablePrefix = "CREATE TABLE paper_runner_leases"
	start := strings.Index(script, tablePrefix)
	if start < 0 {
		return errors.New("paper runner lease migration table is missing")
	}
	expectedTable := strings.SplitN(script[start:], "\n\nINSERT INTO", 2)[0]
	expectedTable = normalizePaperRunnerLeaseSQL(strings.TrimSuffix(strings.TrimSpace(expectedTable), ";"))
	var actualTable string
	if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_runner_leases'`).Scan(&actualTable); err != nil || normalizePaperRunnerLeaseSQL(actualTable) != expectedTable {
		return errors.Join(errors.New("paper runner lease table does not match migration 021"), err)
	}
	var strict int
	if err := q.QueryRowContext(ctx, `SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='paper_runner_leases'`).Scan(&strict); err != nil || strict != 1 {
		return errors.Join(errors.New("paper runner lease strict schema is missing"), err)
	}
	for _, name := range []string{"paper_runner_leases_no_delete", "paper_runner_leases_state_guard"} {
		expected, err := migrationTriggerDefinition(script, name)
		if err != nil {
			return err
		}
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND tbl_name='paper_runner_leases' AND name=?`, name).Scan(&actual); err != nil || normalizePaperRunnerLeaseSQL(actual) != expected {
			return errors.Join(fmt.Errorf("paper runner lease trigger %s does not match migration 021", name), err)
		}
	}
	var triggerCount int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND tbl_name='paper_runner_leases'`).Scan(&triggerCount); err != nil || triggerCount != 2 {
		return errors.Join(errors.New("paper runner lease trigger inventory is invalid"), err)
	}
	return nil
}

func normalizePaperRunnerLeaseSQL(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func loadPaperRunnerLease(ctx context.Context, q orderQuerier) (storedPaperRunnerLease, error) {
	var row storedPaperRunnerLease
	var owner, account sql.NullString
	var heartbeat, expires sql.NullInt64
	var selectionEventID, selectedResult sql.NullString
	err := q.QueryRowContext(ctx, `SELECT scope,fencing_token,owner_id,account_ref,heartbeat_at_ns,expires_at_ns,
		strategy_selection_event_id,selected_result_sha256,record_sha256,record_json
		FROM paper_runner_leases WHERE scope=?`, paperRunnerLeaseScope).Scan(
		&row.record.Scope, &row.record.FencingToken, &owner, &account, &heartbeat, &expires,
		&selectionEventID, &selectedResult, &row.recordSHA, &row.recordJSON)
	if err != nil {
		return storedPaperRunnerLease{}, err
	}
	row.record.OwnerID, row.record.AccountRef = owner.String, account.String
	row.record.HeartbeatAtNS, row.record.ExpiresAtNS = heartbeat.Int64, expires.Int64
	row.record.StrategySelectionEventID, row.record.SelectedResultSHA256 = selectionEventID.String, selectedResult.String
	if err := validateStoredPaperRunnerLease(ctx, q, row); err != nil {
		return storedPaperRunnerLease{}, err
	}
	return row, nil
}

func validateStoredPaperRunnerLease(ctx context.Context, q orderQuerier, row storedPaperRunnerLease) error {
	var decoded paperRunnerLeaseRecord
	if err := json.Unmarshal([]byte(row.recordJSON), &decoded); err != nil {
		return err
	}
	canonical, actualSHA, err := orderJSONHash(decoded)
	if err != nil || string(canonical) != row.recordJSON || actualSHA != row.recordSHA || decoded != row.record ||
		row.record.Scope != paperRunnerLeaseScope || row.record.FencingToken < 0 {
		return errors.New("paper runner lease canonical record mismatch")
	}
	if err := domain.ValidateRunnerLeaseTuple(row.record); err != nil {
		return err
	}
	if row.record.OwnerID == "" {
		return nil
	}
	var selected string
	if err := q.QueryRowContext(ctx, `SELECT selected_result_sha256 FROM strategy_selection_events WHERE event_id=?`, row.record.StrategySelectionEventID).Scan(&selected); err != nil || selected != row.record.SelectedResultSHA256 {
		return errors.Join(errors.New("paper runner lease strategy binding is invalid"), err)
	}
	return nil
}

func updatePaperRunnerLeaseTx(ctx context.Context, tx *transaction, previous, next paperRunnerLeaseRecord) error {
	raw, hash, err := orderJSONHash(next)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE paper_runner_leases SET
		fencing_token=?,owner_id=?,account_ref=?,heartbeat_at_ns=?,expires_at_ns=?,strategy_selection_event_id=?,selected_result_sha256=?,record_sha256=?,record_json=?
		WHERE scope=? AND fencing_token=? AND owner_id IS ? AND account_ref IS ? AND heartbeat_at_ns IS ? AND expires_at_ns IS ?
		AND strategy_selection_event_id IS ? AND selected_result_sha256 IS ?`,
		next.FencingToken, nullable(next.OwnerID), nullable(next.AccountRef), nullableInt64(next.HeartbeatAtNS), nullableInt64(next.ExpiresAtNS),
		nullable(next.StrategySelectionEventID), nullable(next.SelectedResultSHA256), hash, string(raw), paperRunnerLeaseScope,
		previous.FencingToken, nullable(previous.OwnerID), nullable(previous.AccountRef), nullableInt64(previous.HeartbeatAtNS), nullableInt64(previous.ExpiresAtNS),
		nullable(previous.StrategySelectionEventID), nullable(previous.SelectedResultSHA256))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("paper runner lease was lost")
	}
	return nil
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

var paperRunnerTimeNS = domain.PaperRunnerTimeNS

var paperRunnerExpiryNS = domain.PaperRunnerExpiryNS

var paperRunnerClaimFromRecord = application.PaperRunnerClaimFromRecord

var paperRunnerRecordFromClaim = application.PaperRunnerRecordFromClaim

var paperRunnerClaimMatchesRecord = application.PaperRunnerClaimMatchesRecord

var claimAccountRef = application.ClaimAccountRef
