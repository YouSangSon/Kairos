package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestG38ELegacySchema19BaseReplaysUseOldColumnShapeAndDigest(t *testing.T) {
	ctx := context.Background()
	db := g38ELegacySchema19BaseReplayDB(t)
	defer db.Close()

	record := executionAuthorityRecord{
		EventID: "execution_authority_v19", AccountRef: k2aAccountRef, Armed: true,
		FencingToken: 1, ReasonCode: "manual_arm", RecordedAt: "2026-01-10T00:00:00Z",
	}
	raw, recordSHA, err := orderJSONHash(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO execution_authority_events(
		sequence,event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,record_sha256,record_json,recorded_at
	) VALUES(1,?,?,?,?,?,?,?,?,?,?)`, record.EventID, record.AccountRef, 1, nil, record.FencingToken, nil,
		record.ReasonCode, recordSHA, string(raw), record.RecordedAt); err != nil {
		t.Fatal(err)
	}

	strategy, err := proveStrategyRegistryRecovery(ctx, &database{db})
	if err != nil || strategy.SHA256 != emptySHA256 || strategy.Events != 0 || strategy.CurrentEventID != noStrategySelectionEvent {
		t.Fatalf("schema19 strategy proof=%+v err=%v", strategy, err)
	}
	authoritySHA, authorityCount, err := proveExecutionAuthorityRecovery(ctx, &database{db})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if err := json.NewEncoder(hash).Encode([]any{"execution_authority_events", int64(1), record.AccountRef, record.EventID, 1,
		sql.NullString{}, record.FencingToken, sql.NullString{}, record.ReasonCode, recordSHA, string(raw), record.RecordedAt}); err != nil {
		t.Fatal(err)
	}
	if authorityCount != 1 || authoritySHA != hex.EncodeToString(hash.Sum(nil)) {
		t.Fatalf("schema19 authority proof=(%s,%d), want historical digest", authoritySHA, authorityCount)
	}
}

func g38ELegacySchema19BaseReplayDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "g38e-schema19-base.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_migrations(version INTEGER NOT NULL, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations(version,applied_at) VALUES(19,'2026-01-10T00:00:00Z');
		CREATE TABLE execution_authority_events(
			sequence INTEGER PRIMARY KEY,event_id TEXT NOT NULL,account_ref TEXT NOT NULL,armed INTEGER NOT NULL,
			lease_owner TEXT,fencing_token INTEGER NOT NULL,lease_expires_at TEXT,reason_code TEXT NOT NULL,
			record_sha256 TEXT NOT NULL,record_json TEXT NOT NULL,recorded_at TEXT NOT NULL
		);
		CREATE TABLE strategy_research_evidence(
			sequence INTEGER PRIMARY KEY,result_sha256 TEXT NOT NULL,artifact_sha256 TEXT NOT NULL,strategy_name TEXT NOT NULL,
			strategy_version TEXT NOT NULL,parameter_sha256 TEXT NOT NULL,target TEXT NOT NULL,artifact_json TEXT NOT NULL,recorded_at TEXT NOT NULL
		);
		CREATE TABLE strategy_selection_events(
			sequence INTEGER PRIMARY KEY,event_id TEXT NOT NULL,event_type TEXT NOT NULL,candidate_result_sha256 TEXT,
			expected_current_event_id TEXT NOT NULL,source_event_id TEXT,previous_selected_result_sha256 TEXT NOT NULL,
			selected_result_sha256 TEXT NOT NULL,reason_code TEXT NOT NULL,paper_evaluation_sequence INTEGER NOT NULL,
			record_sha256 TEXT NOT NULL,record_json TEXT NOT NULL,recorded_at TEXT NOT NULL
		);
		CREATE TABLE paper_evaluation_events(
			sequence INTEGER PRIMARY KEY,evaluation_id TEXT NOT NULL,schema_version TEXT NOT NULL,policy_version TEXT NOT NULL,
			account_ref TEXT NOT NULL,strategy_result_sha256 TEXT NOT NULL,strategy_selection_event_id TEXT NOT NULL,
			expected_previous_evaluation_id TEXT NOT NULL,paper_order_state_sha256 TEXT NOT NULL,order_count INTEGER NOT NULL,
			terminal_order_count INTEGER NOT NULL,active_order_count INTEGER NOT NULL,pending_action_count INTEGER NOT NULL,
			decision TEXT NOT NULL,reason_code TEXT NOT NULL,record_sha256 TEXT NOT NULL,record_json TEXT NOT NULL,recorded_at TEXT NOT NULL
		);`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}
