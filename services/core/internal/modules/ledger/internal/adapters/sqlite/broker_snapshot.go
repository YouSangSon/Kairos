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
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
)

type brokerRecoveryProof struct {
	SHA256                     string
	Snapshots, Reconciliations int
}
type brokerScope struct {
	q  queryer
	tx *sql.Tx
}

func (s *Ledger) BrokerWrite(ctx context.Context, work func(ledger.BrokerJournal) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&brokerScope{newQueryer(tx), tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Ledger) BrokerRead(ctx context.Context, work func(ledger.BrokerReader) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&brokerScope{q: newQueryer(tx)}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *brokerScope) PrepareMock(ctx context.Context, expected string) error {
	if err := preflightKiwoomMockSync(ctx, s.q); err != nil {
		return err
	}
	return requireKiwoomMockBinding(ctx, s.q, expected)
}
func (s *brokerScope) FindSnapshot(ctx context.Context, snapshot *ledgerdomain.BrokerSnapshot) (ledger.StoredBrokerSnapshot, error) {
	_, snapshotSHA, err := codec.JSONHash(snapshot)
	if err != nil {
		return ledger.StoredBrokerSnapshot{}, err
	}
	var snapshotID, priorSHA, priorJSON string
	err = s.q.QueryRowContext(ctx, `SELECT snapshot_id,snapshot_sha256,snapshot_json FROM broker_snapshots
  WHERE provider='kiwoom' AND environment=? AND exchange=? AND account_ref=? AND fetched_at=?`, snapshot.Environment, snapshot.Exchange, snapshot.AccountRef, snapshot.FetchedAt).Scan(&snapshotID, &priorSHA, &priorJSON)
	if err != nil {
		return ledger.StoredBrokerSnapshot{}, notFound(err)
	}
	if priorSHA != snapshotSHA {
		return ledger.StoredBrokerSnapshot{}, errors.New("fetched_at was already used with a different broker snapshot")
	}
	stored, err := decodeStoredBrokerSnapshot(priorJSON, priorSHA)
	if err != nil {
		return ledger.StoredBrokerSnapshot{}, err
	}
	if !codec.SafeID(snapshotID) || !ledgerdomain.SameBrokerSnapshotIdentity(stored, snapshot) {
		return ledger.StoredBrokerSnapshot{}, errors.New("stored broker snapshot metadata mismatch")
	}
	return ledger.StoredBrokerSnapshot{ID: snapshotID, SHA256: priorSHA, Snapshot: stored}, nil
}
func (s *brokerScope) RecordSnapshot(ctx context.Context, snapshot *ledgerdomain.BrokerSnapshot, id, recordedAt string) (ledger.StoredBrokerSnapshot, error) {
	if !codec.SafeID(id) {
		return ledger.StoredBrokerSnapshot{}, errors.New("broker snapshot identifier is invalid")
	}
	if !codec.CanonicalUTC(recordedAt) {
		return ledger.StoredBrokerSnapshot{}, errors.New("broker snapshot recorded_at is invalid")
	}
	raw, sha, err := codec.JSONHash(snapshot)
	if err != nil {
		return ledger.StoredBrokerSnapshot{}, err
	}
	result, err := s.tx.ExecContext(ctx, `INSERT OR IGNORE INTO broker_snapshots(
  snapshot_id,provider,environment,exchange,account_ref,fetched_at,snapshot_sha256,snapshot_json,recorded_at
 ) VALUES(?,'kiwoom',?,?,?,?,?,?,?)`, id, snapshot.Environment, snapshot.Exchange, snapshot.AccountRef, snapshot.FetchedAt, sha, string(raw), recordedAt)
	if err != nil {
		return ledger.StoredBrokerSnapshot{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ledger.StoredBrokerSnapshot{}, err
	}
	if count == 1 {
		stored, err := decodeStoredBrokerSnapshot(string(raw), sha)
		return ledger.StoredBrokerSnapshot{ID: id, SHA256: sha, Snapshot: stored}, err
	}
	stored, err := s.FindSnapshot(ctx, snapshot)
	if errors.Is(err, contracts.ErrNotFound) {
		return ledger.StoredBrokerSnapshot{}, errors.New("broker snapshot identifier collided with another record")
	}
	return stored, err
}
func (s *brokerScope) FindReconciliation(ctx context.Context, snapshot ledger.StoredBrokerSnapshot, account string, revision int64) (*ledgerdomain.BrokerReconciliationRecord, error) {
	var sha, raw, at string
	err := s.q.QueryRowContext(ctx, `SELECT record_sha256,record_json,recorded_at FROM broker_snapshot_reconciliations
  WHERE snapshot_id=? AND ledger_account_id=? AND ledger_revision=?`, snapshot.ID, account, revision).Scan(&sha, &raw, &at)
	if err != nil {
		return nil, notFound(err)
	}
	record, err := decodeBrokerReconciliation(raw, sha, snapshot.Snapshot)
	if err != nil {
		return nil, err
	}
	if err := requireReconciliationIdentity(record, snapshot.ID, account, revision, at); err != nil {
		return nil, err
	}
	return record, nil
}
func (s *brokerScope) RecordReconciliation(ctx context.Context, snapshot ledger.StoredBrokerSnapshot, revision int64, record *ledgerdomain.BrokerReconciliationRecord) (*ledgerdomain.BrokerReconciliationRecord, error) {
	raw, sha, err := codec.JSONHash(record)
	if err != nil {
		return nil, err
	}
	result, err := s.tx.ExecContext(ctx, `INSERT OR IGNORE INTO broker_snapshot_reconciliations(
  reconciliation_id,snapshot_id,ledger_account_id,ledger_revision,record_sha256,record_json,recorded_at
 ) VALUES(?,?,?,?,?,?,?)`, record.ReconciliationID, snapshot.ID, record.LedgerAccountID, revision, sha, string(raw), record.RecordedAt)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 0 {
		return record, nil
	}
	stored, err := s.FindReconciliation(ctx, snapshot, record.LedgerAccountID, revision)
	if errors.Is(err, contracts.ErrNotFound) {
		return nil, errors.New("broker reconciliation identifier collided with another record")
	}
	return stored, err
}
func (s *brokerScope) LedgerPositions(ctx context.Context, account string) (int64, []ledgerdomain.BrokerLedgerEvent, error) {
	if err := requireCurrentLedgerReplay(ctx, s.q); err != nil {
		return 0, nil, err
	}
	var revision int64
	if err := s.q.QueryRowContext(ctx, `SELECT revision FROM ledger_meta WHERE singleton=1`).Scan(&revision); err != nil {
		return 0, nil, err
	}
	rows, err := s.q.QueryContext(ctx, `SELECT type,symbol,quantity,occurred_at FROM events
  WHERE account_id=? AND currency='KRW' AND type IN ('BUY','SELL') ORDER BY rtrim(occurred_at,'Z'),sequence`, account)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	events := []ledgerdomain.BrokerLedgerEvent{}
	for rows.Next() {
		var event ledgerdomain.BrokerLedgerEvent
		if err := rows.Scan(&event.Type, &event.Symbol, &event.Quantity, &event.OccurredAt); err != nil {
			return 0, nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	return revision, events, nil
}
func (s *brokerScope) LatestSnapshot(ctx context.Context, environment KiwoomEnvironment, exchange KiwoomExchange, accountRef string) (ledger.StoredBrokerReconciliation, error) {
	var snapshotID, ledgerAccountID, fetchedAt, snapshotSHA, snapshotJSON, recordSHA, recordJSON, recordRecordedAt string
	var ledgerRevision int64
	err := s.q.QueryRowContext(ctx, `SELECT s.snapshot_id,r.ledger_account_id,r.ledger_revision,s.fetched_at,
		s.snapshot_sha256,s.snapshot_json,r.record_sha256,r.record_json,r.recorded_at
		FROM broker_snapshots s JOIN broker_snapshot_reconciliations r ON r.snapshot_id=s.snapshot_id
		WHERE s.provider='kiwoom' AND s.environment=? AND s.exchange=? AND s.account_ref=?
		ORDER BY s.fetched_at DESC,s.sequence DESC,r.ledger_revision DESC,r.sequence DESC LIMIT 1`,
		environment, exchange, accountRef).Scan(&snapshotID, &ledgerAccountID, &ledgerRevision, &fetchedAt, &snapshotSHA, &snapshotJSON, &recordSHA, &recordJSON, &recordRecordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.StoredBrokerReconciliation{}, contracts.ErrNotFound
	}
	if err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	snapshot, err := decodeStoredBrokerSnapshot(snapshotJSON, snapshotSHA)
	if err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	record, err := decodeBrokerReconciliation(recordJSON, recordSHA, snapshot)
	if err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	if !codec.SafeID(snapshotID) || snapshot.Environment != ledgerdomain.BrokerEnvironment(environment) || snapshot.Exchange != ledgerdomain.BrokerExchange(exchange) || snapshot.AccountRef != accountRef || snapshot.FetchedAt != fetchedAt {
		return ledger.StoredBrokerReconciliation{}, errors.New("stored broker snapshot metadata mismatch")
	}
	if err := requireReconciliationIdentity(record, snapshotID, ledgerAccountID, ledgerRevision, recordRecordedAt); err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	return ledger.StoredBrokerReconciliation{Snapshot: ledger.StoredBrokerSnapshot{ID: snapshotID, SHA256: snapshotSHA, Snapshot: snapshot}, Record: record}, nil
}

func (s *brokerScope) LatestReconciliation(ctx context.Context) (ledger.StoredBrokerReconciliation, error) {
	var snapshotID, accountRef, fetchedAt, snapshotSHA, snapshotJSON string
	var environment KiwoomEnvironment
	var exchange KiwoomExchange
	if err := s.q.QueryRowContext(ctx, `SELECT snapshot_id,environment,exchange,account_ref,fetched_at,snapshot_sha256,snapshot_json
		FROM broker_snapshots WHERE provider='kiwoom' AND exchange='KRX'
		ORDER BY fetched_at DESC,sequence DESC LIMIT 1`).Scan(
		&snapshotID, &environment, &exchange, &accountRef, &fetchedAt, &snapshotSHA, &snapshotJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ledger.StoredBrokerReconciliation{}, contracts.ErrNotFound
		}
		return ledger.StoredBrokerReconciliation{}, err
	}
	snapshot, err := decodeStoredBrokerSnapshot(snapshotJSON, snapshotSHA)
	if err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	if !codec.SafeID(snapshotID) || snapshot.Environment != ledgerdomain.BrokerEnvironment(environment) || snapshot.Exchange != ledgerdomain.BrokerExchange(exchange) ||
		snapshot.AccountRef != accountRef || snapshot.FetchedAt != fetchedAt {
		return ledger.StoredBrokerReconciliation{}, errors.New("stored broker snapshot metadata mismatch")
	}
	var ledgerAccountID, recordSHA, recordJSON, recordRecordedAt string
	var ledgerRevision int64
	if err := s.q.QueryRowContext(ctx, `SELECT ledger_account_id,ledger_revision,record_sha256,record_json,recorded_at
		FROM broker_snapshot_reconciliations WHERE snapshot_id=?
		ORDER BY ledger_revision DESC,sequence DESC LIMIT 1`, snapshotID).Scan(
		&ledgerAccountID, &ledgerRevision, &recordSHA, &recordJSON, &recordRecordedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ledger.StoredBrokerReconciliation{}, errors.New("latest broker snapshot has no reconciliation")
		}
		return ledger.StoredBrokerReconciliation{}, err
	}
	record, err := decodeBrokerReconciliation(recordJSON, recordSHA, snapshot)
	if err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	if err := requireReconciliationIdentity(record, snapshotID, ledgerAccountID, ledgerRevision, recordRecordedAt); err != nil {
		return ledger.StoredBrokerReconciliation{}, err
	}
	return ledger.StoredBrokerReconciliation{Snapshot: ledger.StoredBrokerSnapshot{ID: snapshotID, SHA256: snapshotSHA, Snapshot: snapshot}, Record: record}, nil
}
func requireReconciliationIdentity(record *ledgerdomain.BrokerReconciliationRecord, snapshotID, ledgerAccountID string, ledgerRevision int64, recordedAt string) error {
	if record.SnapshotID != snapshotID || record.LedgerAccountID != ledgerAccountID || record.LedgerRevision != revision(ledgerRevision) || record.RecordedAt != recordedAt {
		return errors.New("stored broker reconciliation metadata mismatch")
	}
	return nil
}

func decodeStoredBrokerSnapshot(raw, expectedSHA string) (*ledgerdomain.BrokerSnapshot, error) {
	var snapshot ledgerdomain.BrokerSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, err
	}
	canonical, actualSHA, err := codec.JSONHash(&snapshot)
	if err != nil || string(canonical) != raw {
		return nil, errors.New("stored broker snapshot is not canonical")
	}
	if actualSHA != expectedSHA {
		return nil, errors.New("stored broker snapshot hash mismatch")
	}
	if err := ledgerdomain.ValidateBrokerSnapshot(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func decodeBrokerReconciliation(raw, expectedSHA string, snapshot *ledgerdomain.BrokerSnapshot) (*ledgerdomain.BrokerReconciliationRecord, error) {
	var record ledgerdomain.BrokerReconciliationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, err
	}
	canonical, actualSHA, err := codec.JSONHash(&record)
	if err != nil || string(canonical) != raw {
		return nil, errors.New("stored broker reconciliation is not canonical")
	}
	if actualSHA != expectedSHA {
		return nil, errors.New("stored broker reconciliation hash mismatch")
	}
	if err := ledgerdomain.ValidateBrokerReconciliation(&record, snapshot); err != nil {
		return nil, err
	}
	return &record, nil
}

func proveBrokerRecovery(ctx context.Context, q orderQuerier) (brokerRecoveryProof, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	snapshots := map[string]struct {
		snapshot *ledgerdomain.BrokerSnapshot
		sha      string
	}{}
	rows, err := q.QueryContext(ctx, `SELECT sequence,snapshot_id,provider,environment,exchange,account_ref,fetched_at,
		snapshot_sha256,snapshot_json,recorded_at FROM broker_snapshots ORDER BY sequence`)
	if err != nil {
		return brokerRecoveryProof{}, err
	}
	for rows.Next() {
		var sequence int64
		var snapshotID, provider, environment, exchange, accountRef, fetchedAt, snapshotSHA, snapshotJSON, recordedAt string
		if err := rows.Scan(&sequence, &snapshotID, &provider, &environment, &exchange, &accountRef, &fetchedAt,
			&snapshotSHA, &snapshotJSON, &recordedAt); err != nil {
			rows.Close()
			return brokerRecoveryProof{}, err
		}
		snapshot, err := decodeStoredBrokerSnapshot(snapshotJSON, snapshotSHA)
		if err != nil {
			rows.Close()
			return brokerRecoveryProof{}, fmt.Errorf("decode broker snapshot %q: %w", snapshotID, err)
		}
		if !codec.SafeID(snapshotID) || provider != "kiwoom" || string(snapshot.Environment) != environment || string(snapshot.Exchange) != exchange ||
			snapshot.AccountRef != accountRef || snapshot.FetchedAt != fetchedAt || !codec.CanonicalUTC(recordedAt) {
			rows.Close()
			return brokerRecoveryProof{}, fmt.Errorf("broker snapshot %q metadata mismatch", snapshotID)
		}
		if err := encoder.Encode([]any{"broker_snapshots", sequence, snapshotID, provider, environment, exchange, accountRef,
			fetchedAt, snapshotSHA, snapshotJSON, recordedAt}); err != nil {
			rows.Close()
			return brokerRecoveryProof{}, err
		}
		snapshots[snapshotID] = struct {
			snapshot *ledgerdomain.BrokerSnapshot
			sha      string
		}{snapshot, snapshotSHA}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return brokerRecoveryProof{}, err
	}
	if err := rows.Close(); err != nil {
		return brokerRecoveryProof{}, err
	}

	reconciliations := 0
	reconciledSnapshots := map[string]bool{}
	rows, err = q.QueryContext(ctx, `SELECT sequence,reconciliation_id,snapshot_id,ledger_account_id,ledger_revision,
		record_sha256,record_json,recorded_at FROM broker_snapshot_reconciliations ORDER BY sequence`)
	if err != nil {
		return brokerRecoveryProof{}, err
	}
	for rows.Next() {
		var sequence, ledgerRevision int64
		var reconciliationID, snapshotID, ledgerAccountID, recordSHA, recordJSON, recordedAt string
		if err := rows.Scan(&sequence, &reconciliationID, &snapshotID, &ledgerAccountID, &ledgerRevision, &recordSHA, &recordJSON, &recordedAt); err != nil {
			rows.Close()
			return brokerRecoveryProof{}, err
		}
		stored, ok := snapshots[snapshotID]
		if !ok {
			rows.Close()
			return brokerRecoveryProof{}, fmt.Errorf("broker reconciliation %q references unknown snapshot", reconciliationID)
		}
		record, err := decodeBrokerReconciliation(recordJSON, recordSHA, stored.snapshot)
		if err != nil {
			rows.Close()
			return brokerRecoveryProof{}, fmt.Errorf("decode broker reconciliation %q: %w", reconciliationID, err)
		}
		if record.ReconciliationID != reconciliationID || record.SnapshotID != snapshotID || record.LedgerAccountID != ledgerAccountID ||
			record.LedgerRevision != revision(ledgerRevision) || record.RecordedAt != recordedAt {
			rows.Close()
			return brokerRecoveryProof{}, fmt.Errorf("broker reconciliation %q metadata mismatch", reconciliationID)
		}
		if err := encoder.Encode([]any{"broker_snapshot_reconciliations", sequence, reconciliationID, snapshotID,
			ledgerAccountID, ledgerRevision, recordSHA, recordJSON, recordedAt}); err != nil {
			rows.Close()
			return brokerRecoveryProof{}, err
		}
		reconciliations++
		reconciledSnapshots[snapshotID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return brokerRecoveryProof{}, err
	}
	if err := rows.Close(); err != nil {
		return brokerRecoveryProof{}, err
	}
	for snapshotID := range snapshots {
		if !reconciledSnapshots[snapshotID] {
			return brokerRecoveryProof{}, fmt.Errorf("broker snapshot %q has no reconciliation", snapshotID)
		}
	}
	return brokerRecoveryProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Snapshots: len(snapshots), Reconciliations: reconciliations}, nil
}
