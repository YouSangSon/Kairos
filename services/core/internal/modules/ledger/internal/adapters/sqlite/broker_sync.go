package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/migrations"
	"strings"
)

func preflightKiwoomMockSync(ctx context.Context, q orderQuerier) error {
	if err := requireSchemaContext(ctx, q); err != nil {
		return err
	}
	if err := requireKiwoomMockJournalSchema(ctx, q); err != nil {
		return err
	}
	if err := proveLedgerReplayState(ctx, q, false); err != nil {
		return err
	}
	if _, _, err := (&Service{}).proveLedgerEvents(ctx, q); err != nil {
		return err
	}
	_, err := proveBrokerRecovery(ctx, q)
	return err
}

func requireKiwoomMockJournalSchema(ctx context.Context, q orderQuerier) error {
	script, err := migrations.Files.ReadFile("migrations/003_broker_snapshots.sql")
	if err != nil {
		return err
	}
	for _, object := range []struct{ name, kind, table string }{
		{"broker_snapshots", "TABLE", "broker_snapshots"},
		{"broker_snapshots_latest_idx", "INDEX", "broker_snapshots"},
		{"broker_snapshots_no_update", "TRIGGER", "broker_snapshots"},
		{"broker_snapshots_no_delete", "TRIGGER", "broker_snapshots"},
		{"broker_snapshot_reconciliations", "TABLE", "broker_snapshot_reconciliations"},
		{"broker_snapshot_reconciliations_latest_idx", "INDEX", "broker_snapshot_reconciliations"},
		{"broker_snapshot_reconciliations_no_update", "TRIGGER", "broker_snapshot_reconciliations"},
		{"broker_snapshot_reconciliations_no_delete", "TRIGGER", "broker_snapshot_reconciliations"},
		{"events_no_update", "TRIGGER", "events"},
		{"events_no_delete", "TRIGGER", "events"},
	} {
		expected := ""
		// shortcut: controlled migration objects are blank-line-separated; use a SQL parser if that format changes.
		for _, block := range strings.Split(string(script), "\n\n") {
			definition := strings.Join(strings.Fields(block), " ")
			if strings.HasPrefix(definition, "CREATE "+object.kind+" "+object.name+" ") && strings.HasSuffix(definition, ";") {
				expected = strings.TrimSuffix(definition, ";")
				break
			}
		}
		var actual string
		if expected == "" || q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name=?`, object.name, strings.ToLower(object.kind), object.table).Scan(&actual) != nil || strings.Join(strings.Fields(actual), " ") != expected {
			return errors.New("kiwoom mock physical journal schema changed")
		}
	}
	for _, table := range []string{"broker_snapshots", "broker_snapshot_reconciliations", "events"} {
		var shadows int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_temp_master WHERE name=? OR tbl_name=?`, table, table).Scan(&shadows); err != nil {
			return err
		}
		if shadows != 0 {
			return errors.New("kiwoom mock temporary schema shadows the journal")
		}
	}
	for _, table := range []string{"broker_snapshots", "broker_snapshot_reconciliations"} {
		var objects int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger')`, table).Scan(&objects); err != nil {
			return err
		}
		if objects != 5 {
			return errors.New("kiwoom mock physical journal inventory changed")
		}
	}
	return nil
}

func requireKiwoomMockBinding(ctx context.Context, q orderQuerier, expectedAccount string) error {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT environment,account_ref FROM broker_snapshots WHERE provider='kiwoom' AND exchange='KRX'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var environment KiwoomEnvironment
		var account string
		if err := rows.Scan(&environment, &account); err != nil {
			return err
		}
		count++
		if count > 1 || environment != KiwoomMock || (expectedAccount != "" && account != expectedAccount) {
			return errors.New("stored Kiwoom KRX history has a different account binding")
		}
	}
	return rows.Err()
}

func (s *Service) recordKiwoomMockSnapshot(ctx context.Context, ledgerAccount, expectedAccount string, snapshot *KiwoomSnapshot) (*BrokerKnownGoodSnapshot, error) {
	if ledgerAccount != "account-main" || !orderAlias(expectedAccount, "account") || snapshot == nil ||
		snapshot.Environment != KiwoomMock || snapshot.AccountRef != expectedAccount {
		return nil, errors.New("invalid Kiwoom mock account binding")
	}
	if err := validateKiwoomSnapshot(snapshot); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := preflightKiwoomMockSync(ctx, newQueryer(tx)); err != nil {
		return nil, err
	}
	if err := requireKiwoomMockBinding(ctx, newQueryer(tx), expectedAccount); err != nil {
		return nil, err
	}
	stored, err := s.appendKiwoomSnapshot(ctx, tx, ledgerAccount, snapshot)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}
