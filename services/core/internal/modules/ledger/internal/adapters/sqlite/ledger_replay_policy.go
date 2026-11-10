package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/codec"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/migrations"
	"strings"
	"time"
)

const legacyFIFOCostBasisPolicy = "fifo_exact_else_half_even_residual_8_v1"

func requireCurrentLedgerReplay(ctx context.Context, q queryer) error {
	var policy string
	var revision int64
	if err := q.QueryRowContext(ctx, `SELECT policy,(SELECT revision FROM ledger_meta WHERE singleton=1) FROM ledger_replay_state WHERE singleton=1`).Scan(&policy, &revision); err != nil {
		return fmt.Errorf("ledger replay state: %w", err)
	}
	if policy != fifoCostBasisPolicy {
		return errors.New("ledger replay upgrade required; preview and activate the corrected replay before normal use")
	}
	return requireLedgerRevision(ctx, q, revision)
}

func requireLedgerRevision(ctx context.Context, q queryer, revision int64) error {
	var first, last, count int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MIN(sequence),0),COALESCE(MAX(sequence),0),COUNT(*) FROM events`).Scan(&first, &last, &count); err != nil {
		return err
	}
	if revision < 0 || count != revision || last != revision || (count > 0 && first != 1) {
		return errors.New("ledger replay revision does not match contiguous events")
	}
	return nil
}

func ledgerPrefixSHA256(ctx context.Context, q queryer, revision int64) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT sequence,event_id,source_event_id,account_id,type,occurred_at,instrument_id,symbol,quantity,price,fee,currency,amount,counter_currency,counter_amount,corrects_source_event_id,receipt_id,recorded_at FROM events WHERE sequence<=? ORDER BY sequence`, revision)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	var count int64
	for rows.Next() {
		values := make([]any, 18)
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return "", err
		}
		count++
		if values[0] != count {
			return "", errors.New("ledger replay prefix sequence is not contiguous")
		}
		if err := encoder.Encode(values); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count != revision {
		return "", errors.New("ledger replay prefix revision mismatch")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type replayScope struct {
	*ledgerReader
	tx *sql.Tx
}

func (s *Ledger) ReplayRead(ctx context.Context, work func(ledger.ReplayReader) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&replayScope{ledgerReader: &ledgerReader{newQueryer(tx)}}); err != nil {
		return err
	}
	return nil
}
func (s *Ledger) ReplayWrite(ctx context.Context, work func(ledger.ReplayJournal) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&replayScope{&ledgerReader{newQueryer(tx)}, tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *replayScope) CaptureReplayOrigin(ctx context.Context) (ledger.ReplayCapture, error) {
	if err := requireSchemaContext(ctx, s.q); err != nil {
		return ledger.ReplayCapture{}, err
	}
	if err := proveLedgerReplayState(ctx, s.q, true); err != nil {
		return ledger.ReplayCapture{}, err
	}
	var result ledger.ReplayCapture
	err := s.q.QueryRowContext(ctx, `SELECT policy,legacy_revision FROM ledger_replay_state WHERE singleton=1`).Scan(&result.Policy, &result.LegacyRevision)
	return result, err
}
func (s *replayScope) VerifiedLedgerRevision(ctx context.Context) (int64, error) {
	revision, _, err := (&Service{}).proveLedgerEvents(ctx, s.q)
	return revision, err
}
func (s *replayScope) ReplayEvidence(ctx context.Context, revision int64, old, corrected *PortfolioSnapshot) (ledger.ReplayEvidence, error) {
	_, oldSHA, err := codec.JSONHash(old)
	if err != nil {
		return ledger.ReplayEvidence{}, err
	}
	_, correctedSHA, err := codec.JSONHash(corrected)
	if err != nil {
		return ledger.ReplayEvidence{}, err
	}
	eventsSHA, err := ledgerPrefixSHA256(ctx, s.q, revision)
	if err != nil {
		return ledger.ReplayEvidence{}, err
	}
	return ledger.ReplayEvidence{EventsSHA256: eventsSHA, LegacySnapshotSHA256: oldSHA, SnapshotSHA256: correctedSHA}, nil
}
func (s *replayScope) RecordReplayTransition(ctx context.Context, preview *ledgerReplayPreview, activatedAt string) error {
	if _, err := s.tx.ExecContext(ctx, `INSERT INTO ledger_replay_transition VALUES(1,?,?,?,?,?,?)`, preview.LedgerRevision, preview.Snapshot.RecordedAt,
		preview.EventsSHA256, preview.LegacySnapshotSHA256, preview.SnapshotSHA256, activatedAt); err != nil {
		return err
	}
	if _, err := s.tx.ExecContext(ctx, `UPDATE ledger_replay_state SET policy=? WHERE singleton=1`, fifoCostBasisPolicy); err != nil {
		return err
	}
	return proveLedgerReplayState(ctx, s.q, false)
}
func ledgerReplayAt(ctx context.Context, q queryer, revision int64, recordedAt string) (*ledgerReplayPreview, error) {
	return ledger.ReplayAt(ctx, &replayScope{ledgerReader: &ledgerReader{q}}, revision, recordedAt)
}

func proveLedgerReplayState(ctx context.Context, q queryer, archival bool) error {
	if err := requireLedgerReplaySchema(ctx, q); err != nil {
		return err
	}
	var policy string
	var legacyRevision, currentRevision, count int64
	if err := q.QueryRowContext(ctx, `SELECT policy,legacy_revision,(SELECT revision FROM ledger_meta WHERE singleton=1),(SELECT COUNT(*) FROM ledger_replay_transition) FROM ledger_replay_state WHERE singleton=1`).Scan(&policy, &legacyRevision, &currentRevision, &count); err != nil {
		return err
	}
	if legacyRevision < 0 || legacyRevision > currentRevision {
		return errors.New("ledger replay origin revision is invalid")
	}
	if err := requireLedgerRevision(ctx, q, currentRevision); err != nil {
		return err
	}
	if policy == legacyFIFOCostBasisPolicy {
		if !archival || legacyRevision == 0 || currentRevision != legacyRevision || count != 0 {
			return errors.New("legacy replay state is not eligible for current activation")
		}
		return nil
	}
	if policy != fifoCostBasisPolicy || (legacyRevision == 0 && count != 0) || (legacyRevision > 0 && count != 1) {
		return errors.New("ledger replay policy or transition count is invalid")
	}
	if legacyRevision == 0 {
		return nil
	}
	var transitionRevision int64
	var recordedAt, eventsSHA, oldSHA, correctedSHA, activatedAt string
	if err := q.QueryRowContext(ctx, `SELECT ledger_revision,ledger_recorded_at,events_sha256,legacy_snapshot_sha256,snapshot_sha256,activated_at FROM ledger_replay_transition WHERE singleton=1`).Scan(&transitionRevision, &recordedAt, &eventsSHA, &oldSHA, &correctedSHA, &activatedAt); err != nil {
		return err
	}
	if transitionRevision != legacyRevision || !codec.CanonicalUTC(recordedAt) || !codec.CanonicalUTC(activatedAt) {
		return errors.New("ledger replay transition metadata is invalid")
	}
	recordedTime, _ := time.Parse(time.RFC3339Nano, recordedAt)
	activatedTime, _ := time.Parse(time.RFC3339Nano, activatedAt)
	if activatedTime.Before(recordedTime) {
		return errors.New("ledger replay activation precedes its captured ledger")
	}
	preview, err := ledgerReplayAt(ctx, q, legacyRevision, recordedAt)
	if err != nil {
		return err
	}
	if preview.Snapshot.RecordedAt != recordedAt || preview.EventsSHA256 != eventsSHA || preview.LegacySnapshotSHA256 != oldSHA || preview.SnapshotSHA256 != correctedSHA {
		return errors.New("ledger replay transition proof mismatch")
	}
	return nil
}

func requireLedgerReplaySchema(ctx context.Context, q queryer) error {
	script, err := migrations.Files.ReadFile("migrations/022_ledger_replay_policy.sql")
	if err != nil {
		return err
	}
	objects := map[string]string{
		"ledger_replay_state": "TABLE", "ledger_replay_transition": "TABLE",
		"ledger_replay_transition_no_update": "TRIGGER", "ledger_replay_transition_no_delete": "TRIGGER",
		"ledger_replay_state_no_delete": "TRIGGER", "ledger_replay_state_transition": "TRIGGER", "events_replay_policy_guard": "TRIGGER",
		"ledger_replay_state_no_replace": "TRIGGER", "ledger_replay_transition_insert_guard": "TRIGGER",
	}
	normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	for name, kind := range objects {
		prefix := "CREATE " + kind + " " + name + " "
		_, after, found := strings.Cut(string(script), prefix)
		terminator := "END;"
		if kind == "TABLE" {
			terminator = ") STRICT;"
		}
		body, _, ended := strings.Cut(after, terminator)
		if !found || !ended {
			return errors.New("ledger replay migration definition is incomplete")
		}
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=? AND type=?`, name, strings.ToLower(kind)).Scan(&actual); err != nil {
			return err
		}
		expected := strings.TrimSuffix(prefix+body+terminator, ";")
		if normalize(actual) != normalize(expected) {
			return fmt.Errorf("ledger replay schema object %s changed", name)
		}
	}
	return nil
}
