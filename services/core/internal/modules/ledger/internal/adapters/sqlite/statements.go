package sqlite

import "omni-folio/services/core/internal/storage/observation"

func ReadStatements() []observation.Statement {
	return []observation.Statement{
		{SQL: `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name=?`, Arguments: 3, Objects: []string{"broker_snapshots", "broker_snapshots_latest_idx", "broker_snapshots_no_update", "broker_snapshots_no_delete", "broker_snapshot_reconciliations", "broker_snapshot_reconciliations_latest_idx", "broker_snapshot_reconciliations_no_update", "broker_snapshot_reconciliations_no_delete", "events_no_update", "events_no_delete", "events"}, ObjectArguments: []int{0, 2}},
		{SQL: `SELECT COUNT(*) FROM sqlite_temp_master WHERE name=? OR tbl_name=?`, Arguments: 2, Objects: []string{"broker_snapshots", "broker_snapshots_latest_idx", "broker_snapshots_no_update", "broker_snapshots_no_delete", "broker_snapshot_reconciliations", "broker_snapshot_reconciliations_latest_idx", "broker_snapshot_reconciliations_no_update", "broker_snapshot_reconciliations_no_delete", "events_no_update", "events_no_delete", "events"}, ObjectArguments: []int{0, 1}},
		{SQL: `SELECT COUNT(*) FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger')`, Arguments: 1, Objects: []string{"broker_snapshots", "broker_snapshots_latest_idx", "broker_snapshots_no_update", "broker_snapshots_no_delete", "broker_snapshot_reconciliations", "broker_snapshot_reconciliations_latest_idx", "broker_snapshot_reconciliations_no_update", "broker_snapshot_reconciliations_no_delete", "events_no_update", "events_no_delete", "events"}, ObjectArguments: []int{0}},

		{SQL: `SELECT occurred_at FROM events ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT 1`, Arguments: 0},
		{SQL: `SELECT EXISTS(SELECT 1 FROM events WHERE sequence<=? AND rtrim(recorded_at,'Z')>rtrim(?,'Z'))`, Arguments: 2},
		{SQL: `SELECT e.type,e.occurred_at,COALESCE(e.instrument_id,''),COALESCE(e.symbol,''),COALESCE(e.quantity,''),e.currency,e.amount,COALESCE(e.counter_currency,''),COALESCE(e.counter_amount,'') FROM events e
 WHERE e.type<>'CASH_VOID' AND NOT EXISTS (SELECT 1 FROM events c WHERE c.type='CASH_VOID' AND c.account_id=e.account_id AND c.corrects_source_event_id=e.source_event_id)
 ORDER BY rtrim(e.occurred_at,'Z'),e.sequence`, Arguments: 0},
		{SQL: `SELECT e.occurred_at,e.currency,e.amount FROM events e
 WHERE e.type IN ('DEPOSIT','WITHDRAWAL') AND NOT EXISTS
 (SELECT 1 FROM events correction WHERE correction.account_id=e.account_id AND correction.corrects_source_event_id=e.source_event_id)
 ORDER BY rtrim(e.occurred_at,'Z'),e.sequence`, Arguments: 0},
		{SQL: `SELECT instrument_id,currency,occurred_at FROM events WHERE type='SPLIT' ORDER BY rtrim(occurred_at,'Z'),sequence`, Arguments: 0},
		{SQL: `SELECT instrument_id,symbol,currency FROM events WHERE instrument_id IS NOT NULL AND instrument_id<>''`, Arguments: 0},
		{SQL: `SELECT COALESCE(MIN(sequence),0),COALESCE(MAX(sequence),0),COUNT(*) FROM events`, Arguments: 0},
		{SQL: `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`, Arguments: 0},
		{SQL: `SELECT COUNT(*) FROM events WHERE account_id=? AND corrects_source_event_id=?`, Arguments: 2},
		{SQL: `SELECT event_id,source_event_id,account_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),COALESCE(corrects_source_event_id,'') FROM events WHERE account_id=? AND source_event_id=?`, Arguments: 2},
		{SQL: `SELECT event_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),receipt_id FROM events WHERE sequence<=? ORDER BY occurred_at, sequence`, Arguments: 1},
		{SQL: `SELECT event_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),receipt_id FROM events WHERE sequence<=? ORDER BY rtrim(occurred_at,'Z'), sequence`, Arguments: 1},
		{SQL: `SELECT ledger_account_id,ledger_revision,record_sha256,record_json,recorded_at
		FROM broker_snapshot_reconciliations WHERE snapshot_id=?
		ORDER BY ledger_revision DESC,sequence DESC LIMIT 1`, Arguments: 1},
		{SQL: `SELECT ledger_revision,ledger_recorded_at,events_sha256,legacy_snapshot_sha256,snapshot_sha256,activated_at FROM ledger_replay_transition WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT occurred_at FROM events WHERE sequence=?`, Arguments: 1},
		{SQL: `SELECT policy,(SELECT revision FROM ledger_meta WHERE singleton=1) FROM ledger_replay_state WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT policy,legacy_revision FROM ledger_replay_state WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT policy,legacy_revision,(SELECT revision FROM ledger_meta WHERE singleton=1),(SELECT COUNT(*) FROM ledger_replay_transition) FROM ledger_replay_state WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT preview_json,file_sha256,ledger_revision,can_apply,expires_at FROM previews WHERE preview_id=?`, Arguments: 1},
		{SQL: `SELECT record_sha256,record_json,recorded_at FROM broker_snapshot_reconciliations
			WHERE snapshot_id=? AND ledger_account_id=? AND ledger_revision=?`, Arguments: 3},
		{SQL: `SELECT record_sha256,record_json,recorded_at FROM broker_snapshot_reconciliations
		WHERE snapshot_id=? AND ledger_account_id=? AND ledger_revision=?`, Arguments: 3},
		{SQL: `SELECT recorded_at FROM events WHERE sequence=?`, Arguments: 1},
		{SQL: `SELECT recorded_at FROM ledger_meta WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT request_sha256, receipt_json FROM receipts WHERE idempotency_key=?`, Arguments: 1},
		{SQL: `SELECT revision FROM ledger_meta WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT revision, recorded_at, last_verified_at FROM ledger_meta WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT revision,recorded_at FROM ledger_meta WHERE singleton=1`, Arguments: 0},
		{SQL: `SELECT s.snapshot_id,r.ledger_account_id,r.ledger_revision,s.fetched_at,
		s.snapshot_sha256,s.snapshot_json,r.record_sha256,r.record_json,r.recorded_at
		FROM broker_snapshots s JOIN broker_snapshot_reconciliations r ON r.snapshot_id=s.snapshot_id
		WHERE s.provider='kiwoom' AND s.environment=? AND s.exchange=? AND s.account_ref=?
		ORDER BY s.fetched_at DESC,s.sequence DESC,r.ledger_revision DESC,r.sequence DESC LIMIT 1`, Arguments: 3},
		{SQL: `SELECT sequence,event_id,source_event_id,account_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),COALESCE(corrects_source_event_id,''),receipt_id,recorded_at FROM events ORDER BY sequence`, Arguments: 0},
		{SQL: `SELECT sequence,event_id,source_event_id,account_id,type,occurred_at,instrument_id,symbol,quantity,price,fee,currency,amount,counter_currency,counter_amount,corrects_source_event_id,receipt_id,recorded_at FROM events WHERE sequence<=? ORDER BY sequence`, Arguments: 1},
		{SQL: `SELECT sequence,reconciliation_id,snapshot_id,ledger_account_id,ledger_revision,
		record_sha256,record_json,recorded_at FROM broker_snapshot_reconciliations ORDER BY sequence`, Arguments: 0},
		{SQL: `SELECT sequence,snapshot_id,provider,environment,exchange,account_ref,fetched_at,
		snapshot_sha256,snapshot_json,recorded_at FROM broker_snapshots ORDER BY sequence`, Arguments: 0},
		{SQL: `SELECT snapshot_id,environment,exchange,account_ref,fetched_at,snapshot_sha256,snapshot_json
		FROM broker_snapshots WHERE provider='kiwoom' AND exchange='KRX'
		ORDER BY fetched_at DESC,sequence DESC LIMIT 1`, Arguments: 0},
		{SQL: `SELECT snapshot_id,snapshot_sha256,snapshot_json FROM broker_snapshots
		WHERE provider='kiwoom' AND environment=? AND exchange=? AND account_ref=? AND fetched_at=?`, Arguments: 4},
		{SQL: `SELECT sql FROM sqlite_master WHERE name=? AND type=?`, Arguments: 2, Objects: []string{"ledger_replay_state", "ledger_replay_transition", "ledger_replay_transition_no_update", "ledger_replay_transition_no_delete", "ledger_replay_state_no_delete", "ledger_replay_state_transition", "events_replay_policy_guard", "ledger_replay_state_no_replace", "ledger_replay_transition_insert_guard"}, ObjectArguments: []int{0}},
		{SQL: `SELECT type,occurred_at,recorded_at,symbol,quantity,price,fee,amount
		FROM events WHERE instrument_id=? AND currency=? AND type IN ('BUY','SELL') AND sequence<=?
		ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT ?`, Arguments: 4},
		{SQL: `SELECT type,symbol,quantity,occurred_at FROM events
		WHERE account_id=? AND currency='KRW' AND type IN ('BUY','SELL') ORDER BY rtrim(occurred_at,'Z'),sequence`, Arguments: 1},
	}
}
