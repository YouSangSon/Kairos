package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

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

func TestG38EStartupRejectsTamperedPolicyRecovery(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	event, err := svc.applyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_performance_policy_events_no_update;
		UPDATE paper_performance_policy_events SET record_sha256=? WHERE policy_event_id=?`, emptySHA256, event.PolicyEventID); err != nil {
		t.Fatal(err)
	}
	if err := requireServerStartupRecovery(svc.db); err == nil {
		t.Fatal("pre-listen startup acceptance certified a corrupt G3.8E policy row")
	}
}

func g38ECanonicalV19MigrationDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "canonical-v19.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	files := []string{
		"001_init.sql", "002_orders.sql", "003_broker_snapshots.sql", "004_execution_authority.sql",
		"005_ledger_events.sql", "006_strategy_registry.sql", "007_paper_orders.sql", "008_cash_void.sql",
		"009_fx_exchange.sql", "010_fx_observations.sql", "011_security_price_observations.sql",
		"012_kiwoom_security_price_observations.sql", "013_instrument_listing_events.sql",
		"014_paper_evaluation_events.sql", "015_paper_accounting_sessions.sql", "016_paper_market_signals.sql",
		"017_paper_execution_authorizations.sql", "018_paper_performance_events.sql", "019_paper_strategy_performance_events.sql",
	}
	for index, file := range files {
		version := index + 1
		script, err := migrationFiles.ReadFile("migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if version == 7 {
			if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(string(script)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, "2026-01-10T00:00:00Z"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if version >= 7 {
			var violations int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
				_ = tx.Rollback()
				t.Fatalf("canonical v19 migration %d foreign-key check violations=%d err=%v", version, violations, err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if version == 7 {
			if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

func TestG38EMigration20PreservesNonemptyV19JournalsProofsAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "100"})
	if _, err := svc.evaluatePaperOperations(ctx, k2aAccountRef, performance.SelectedStrategyResultRef, performance.StrategySelectionEventID); err != nil {
		t.Fatal(err)
	}
	order := mustRecordK2AOrder(t, svc, "g38e-migration20-risk-reservation")
	lease := mustK2CLease(t, svc, k2aAccountRef)
	mustAuthorizeK2C(t, svc, order.OrderID, lease.FencingToken)
	downgradePaperPerformancePolicyForTest(t, svc.db)
	beforeCounts := g38EMigration20Counts(t, svc.db)
	for table, count := range beforeCounts {
		if count == 0 {
			t.Fatalf("non-empty v19 fixture has no %s rows", table)
		}
	}
	before, err := captureMigration20Snapshot(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	performanceBefore, err := provePaperStrategyPerformanceRecovery(ctx, svc.db)
	if err != nil || performanceBefore.Events == 0 || performanceBefore.Samples == 0 {
		t.Fatalf("non-empty v19 G3.8D proof=%+v err=%v", performanceBefore, err)
	}
	if err := migrate(svc.db); err != nil {
		t.Fatal(err)
	}
	authorityAfter, authorityCountAfter, err := proveExecutionAuthorityRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	strategyAfter, err := proveStrategyRegistryRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	performanceAfter, err := provePaperStrategyPerformanceRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	afterCounts := g38EMigration20Counts(t, svc.db)
	afterForeignKeys, err := foreignKeysForTest(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	canonicalDB := g38ECanonicalV19MigrationDB(t)
	defer canonicalDB.Close()
	canonicalKeys, err := foreignKeysForTest(ctx, canonicalDB)
	if err != nil || !reflect.DeepEqual(before.ForeignKeys, canonicalKeys) {
		t.Fatalf("nonempty v19 fixture foreign keys differ from canonical migrations: %+v %v", before.ForeignKeys, err)
	}
	expectedForeignKeys := append([]testForeignKey(nil), before.ForeignKeys...)
	expectedForeignKeys = append(expectedForeignKeys,
		testForeignKey{Child: "execution_authority_events", Parent: "paper_performance_policy_events", From: "paper_performance_policy_event_id", To: "policy_event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "strategy_selection_events", Parent: "paper_performance_policy_events", From: "paper_performance_policy_event_id", To: "policy_event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "paper_accounting_sessions", From: "paper_accounting_session_id", To: "session_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "strategy_selection_events", From: "strategy_selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "strategy_research_evidence", From: "selected_strategy_result_ref", To: "result_sha256", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "paper_strategy_performance_events", From: "strategy_performance_id", To: "strategy_performance_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "paper_performance_events", From: "baseline_performance_id", To: "performance_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "paper_performance_events", From: "latest_performance_id", To: "performance_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_performance_policy_events", Parent: "strategy_selection_events", From: "rollback_selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
	)
	expectedForeignKeys = append(expectedForeignKeys,
		testForeignKey{Child: "paper_runner_leases", Parent: "strategy_selection_events", From: "strategy_selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_runner_leases", Parent: "strategy_selection_events", From: "selected_result_sha256", To: "selected_result_sha256", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "strategy_selection_events", Parent: "paper_promotion_decisions", From: "paper_promotion_attempt_id", To: "attempt_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_promotion_decisions", Parent: "paper_promotion_claims", From: "attempt_id", To: "attempt_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		testForeignKey{Child: "paper_promotion_decisions", Parent: "strategy_selection_events", From: "selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
	)
	sort.Slice(expectedForeignKeys, func(i, j int) bool {
		return foreignKeySortKey(expectedForeignKeys[i]) < foreignKeySortKey(expectedForeignKeys[j])
	})
	if authorityAfter != before.AuthoritySHA || authorityCountAfter != before.AuthorityCount ||
		!sameStrategyRegistryProof(before.Strategy, strategyAfter) || performanceBefore != performanceAfter ||
		!reflect.DeepEqual(expectedForeignKeys, afterForeignKeys) ||
		!sameG38EMigration20Counts(beforeCounts, afterCounts) {
		t.Fatalf("v19 migration changed journals/proofs/counts authority=%s/%s events=%d/%d strategy=%+v/%+v performance=%+v/%+v counts=%v/%v",
			before.AuthoritySHA, authorityAfter, before.AuthorityCount, authorityCountAfter, before.Strategy, strategyAfter,
			performanceBefore, performanceAfter, beforeCounts, afterCounts)
	}
	for _, key := range afterForeignKeys {
		if strings.HasSuffix(key.Child, "_new") || strings.HasSuffix(key.Child, "_old") || strings.HasSuffix(key.Parent, "_new") || strings.HasSuffix(key.Parent, "_old") {
			t.Fatalf("temporary migration table remains in foreign key: %+v", key)
		}
	}
	var temporaryTables int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND (name GLOB '*_new' OR name GLOB '*_old')`).Scan(&temporaryTables); err != nil || temporaryTables != 0 {
		t.Fatalf("temporary migration schema remains: %d %v", temporaryTables, err)
	}
}

func g38EMigration20Counts(t testing.TB, db *sql.DB) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for table := range map[string]bool{
		"strategy_selection_events": true, "execution_authority_events": true, "order_idempotency": true,
		"risk_reservations": true, "paper_evaluation_events": true, "paper_accounting_sessions": true,
		"paper_strategy_performance_events": true,
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func sameG38EMigration20Counts(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for table, count := range left {
		if right[table] != count {
			return false
		}
	}
	return true
}

func TestG38EBackupV14CarriesAndVerifiesPolicyRecoveryProof(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	if _, err := svc.applyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID); err != nil {
		t.Fatal(err)
	}
	proof, err := provePaperPerformancePolicyRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "g38e-policy-v14.db")
	golden := writeCurrentSnapshot(t, svc.db)
	manifest, err := createBackup(svc.db, backup, golden, backup+".manifest.json", svc.now, svc.id)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != backupFormat || manifest.SchemaVersion != backupSchema ||
		manifest.PaperPerformancePolicyStateSHA256 != proof.SHA256 || manifest.PaperPerformancePolicyEventCount != proof.Events ||
		manifest.PaperPerformancePolicyActionCount != proof.Actions || manifest.PaperPerformancePolicyAutomaticHaltCount != proof.AutomaticHalts ||
		manifest.VerificationReceipt.PaperPerformancePolicyCheck != "ok" ||
		manifest.VerificationReceipt.CandidatePaperPerformancePolicyStateSHA256 != proof.SHA256 {
		t.Fatalf("v14 policy proof omitted: %+v", manifest)
	}
	if err := verifyManifest(backup, golden, backup+".manifest.json"); err != nil {
		t.Fatal(err)
	}
}

func TestG38EBackupLegacyV13UsesOwnedV20MigrationWithoutChangingSource(t *testing.T) {
	ctx := context.Background()
	svc, _ := g38EPerformanceWindow(t, []string{"100", "100"})
	strategyBefore, err := proveStrategyRegistryRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	authorityBefore, authorityEvents, err := proveExecutionAuthorityRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	performanceBefore, err := provePaperStrategyPerformanceRecovery(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	currentBackup := filepath.Join(t.TempDir(), "g38e-v14-source.db")
	golden := writeCurrentSnapshot(t, svc.db)
	if _, err := createBackup(svc.db, currentBackup, golden, currentBackup+".manifest.json", svc.now, svc.id); err != nil {
		t.Fatal(err)
	}
	downgradePaperPerformancePolicyForTest(t, svc.db)
	legacyBackup := filepath.Join(t.TempDir(), "g38e-v13-source.db")
	if _, err := svc.db.Exec(`VACUUM INTO '` + strings.ReplaceAll(legacyBackup, "'", "''") + `'`); err != nil {
		t.Fatal(err)
	}
	legacy := readJSONMap(t, currentBackup+".manifest.json")
	legacy["format_version"], legacy["schema_version"] = "omni-folio-backup.v13", "omni-folio.sqlite.v19"
	stripBenchmarkProofForLegacy(legacy)
	for _, field := range []string{
		"paper_performance_policy_state_sha256", "paper_performance_policy_event_count",
		"paper_performance_policy_action_count", "paper_performance_policy_automatic_halt_count",
	} {
		delete(legacy, field)
	}
	receipt := legacy["verification_receipt"].(map[string]any)
	delete(receipt, "paper_performance_policy_check")
	delete(receipt, "candidate_paper_performance_policy_state_sha256")
	sha, size, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	legacy["db_sha256"], legacy["size_bytes"], receipt["candidate_db_sha256"] = sha, size, sha
	legacyManifest := filepath.Join(t.TempDir(), "g38e-v13-source.manifest.json")
	legacyReplayGolden := legacyGoldenForTest(t, golden, legacy)
	writeJSONFile(t, legacyManifest, legacy)
	sourceSHA, sourceSize, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	manifestSHA, manifestSize, err := hashFile(legacyManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(legacyBackup, legacyReplayGolden, legacyManifest); err != nil {
		t.Fatal(err)
	}
	afterSHA, afterSize, sourceErr := hashFile(legacyBackup)
	afterManifestSHA, afterManifestSize, manifestErr := hashFile(legacyManifest)
	if sourceErr != nil || manifestErr != nil || sourceSHA != afterSHA || sourceSize != afterSize ||
		manifestSHA != afterManifestSHA || manifestSize != afterManifestSize {
		t.Fatalf("v13 source changed db=(%s,%d)->(%s,%d) manifest=(%s,%d)->(%s,%d) errors=(%v,%v)",
			sourceSHA, sourceSize, afterSHA, afterSize, manifestSHA, manifestSize, afterManifestSHA, afterManifestSize, sourceErr, manifestErr)
	}
	owned := filepath.Join(t.TempDir(), "g38e-v13-owned.db")
	if err := copyFile(legacyBackup, owned); err != nil {
		t.Fatal(err)
	}
	db, err := openExistingDB(owned)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	strategyAfter, strategyErr := proveStrategyRegistryRecovery(ctx, db)
	authorityAfter, authorityCountAfter, authorityErr := proveExecutionAuthorityRecovery(ctx, db)
	performanceAfter, performanceErr := provePaperStrategyPerformanceRecovery(ctx, db)
	policyAfter, policyErr := provePaperPerformancePolicyRecovery(ctx, db)
	closeErr := db.Close()
	if strategyErr != nil || authorityErr != nil || performanceErr != nil || policyErr != nil || closeErr != nil ||
		!sameStrategyRegistryProof(strategyBefore, strategyAfter) || authorityBefore != authorityAfter || authorityEvents != authorityCountAfter ||
		performanceBefore != performanceAfter || policyAfter.SHA256 != emptySHA256 || policyAfter.Events != 0 || policyAfter.Actions != 0 || policyAfter.AutomaticHalts != 0 {
		t.Fatalf("owned v13 migration strategy=%+v/%+v authority=%s/%s counts=%d/%d performance=%+v/%+v policy=%+v errors=(%v,%v,%v,%v,%v)",
			strategyBefore, strategyAfter, authorityBefore, authorityAfter, authorityEvents, authorityCountAfter, performanceBefore, performanceAfter, policyAfter,
			strategyErr, authorityErr, performanceErr, policyErr, closeErr)
	}
	failedSource, err := openExistingDB(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failedSource.Exec(`DROP INDEX execution_authority_latest_idx`); err != nil {
		_ = failedSource.Close()
		t.Fatal(err)
	}
	if err := failedSource.Close(); err != nil {
		t.Fatal(err)
	}
	failureSHA, failureSize, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	legacy["db_sha256"], legacy["size_bytes"], receipt["candidate_db_sha256"] = failureSHA, failureSize, failureSHA
	writeJSONFile(t, legacyManifest, legacy)
	failureManifestSHA, failureManifestSize, err := hashFile(legacyManifest)
	if err != nil {
		t.Fatal(err)
	}
	beforeCandidates, err := filepath.Glob(filepath.Join(os.TempDir(), "omni-folio-restore-legacy.*"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(legacyBackup, legacyReplayGolden, legacyManifest); err == nil {
		t.Fatal("failed v13 candidate migration was accepted")
	}
	afterCandidates, err := filepath.Glob(filepath.Join(os.TempDir(), "omni-folio-restore-legacy.*"))
	if err != nil {
		t.Fatal(err)
	}
	if !sameStringSet(beforeCandidates, afterCandidates) {
		t.Fatalf("failed v13 candidate directory leaked before=%v after=%v", beforeCandidates, afterCandidates)
	}
	failureAfterSHA, failureAfterSize, sourceFailureErr := hashFile(legacyBackup)
	failureAfterManifestSHA, failureAfterManifestSize, manifestFailureErr := hashFile(legacyManifest)
	if sourceFailureErr != nil || manifestFailureErr != nil || failureSHA != failureAfterSHA || failureSize != failureAfterSize ||
		failureManifestSHA != failureAfterManifestSHA || failureManifestSize != failureAfterManifestSize {
		t.Fatalf("failed v13 candidate changed source db=(%s,%d)->(%s,%d) manifest=(%s,%d)->(%s,%d) errors=(%v,%v)",
			failureSHA, failureSize, failureAfterSHA, failureAfterSize, failureManifestSHA, failureManifestSize,
			failureAfterManifestSHA, failureAfterManifestSize, sourceFailureErr, manifestFailureErr)
	}
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
}

func downgradePaperPerformancePolicyForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	downgradeLedgerReplayForTest(t, db)
	var currentVersion int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion); err != nil {
		t.Fatal(err)
	}
	if currentVersion >= 21 {
		if _, err := db.Exec(`DROP TABLE paper_runner_leases;
			DROP INDEX strategy_selection_events_runner_binding_idx;
			DELETE FROM schema_migrations WHERE version=21`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`
		DROP TRIGGER paper_performance_policy_events_no_update;
		DROP TRIGGER paper_performance_policy_events_no_delete;
		DROP TRIGGER paper_performance_policy_events_state_guard;
		DROP TRIGGER execution_authority_events_state_guard;
		DROP TRIGGER strategy_selection_events_state_guard;
		DROP TRIGGER execution_authority_events_no_update;
		DROP TRIGGER execution_authority_events_no_delete;
		DROP TRIGGER strategy_selection_events_no_update;
		DROP TRIGGER strategy_selection_events_no_delete;
		DROP INDEX paper_performance_policy_events_account_selection_idx;
		DROP INDEX execution_authority_latest_idx;
		CREATE TABLE execution_authority_events_v19 (
			sequence INTEGER PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,account_ref TEXT NOT NULL,armed INTEGER NOT NULL CHECK (armed IN (0,1)),
			lease_owner TEXT,fencing_token INTEGER NOT NULL CHECK (fencing_token>0),lease_expires_at TEXT,
			reason_code TEXT NOT NULL CHECK (reason_code IN ('manual_arm','manual_halt','lease_acquired')),
			record_sha256 TEXT NOT NULL,record_json TEXT NOT NULL,recorded_at TEXT NOT NULL,
			UNIQUE(account_ref,fencing_token),CHECK((lease_owner IS NULL AND lease_expires_at IS NULL) OR (armed=1 AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)),CHECK(armed=1 OR lease_owner IS NULL)
		) STRICT;
		INSERT INTO execution_authority_events_v19(sequence,event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,record_sha256,record_json,recorded_at)
		SELECT sequence,event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,record_sha256,record_json,recorded_at FROM execution_authority_events;
		CREATE TABLE strategy_selection_events_v19 (
			sequence INTEGER PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,event_type TEXT NOT NULL CHECK(event_type IN ('SELECT','ROLLBACK')),
			candidate_result_sha256 TEXT REFERENCES strategy_research_evidence(result_sha256),expected_current_event_id TEXT NOT NULL,
			source_event_id TEXT REFERENCES strategy_selection_events(event_id),previous_selected_result_sha256 TEXT NOT NULL,selected_result_sha256 TEXT NOT NULL,
			reason_code TEXT NOT NULL CHECK(reason_code IN ('manual_selection','manual_rollback')),
			paper_evaluation_sequence INTEGER NOT NULL DEFAULT 0 CHECK(paper_evaluation_sequence>=0),record_sha256 TEXT NOT NULL CHECK(length(record_sha256)=64 AND record_sha256 NOT GLOB '*[^0-9a-f]*'),record_json TEXT NOT NULL,recorded_at TEXT NOT NULL,
			CHECK((event_type='SELECT' AND candidate_result_sha256 IS NOT NULL AND source_event_id IS NULL AND reason_code='manual_selection') OR (event_type='ROLLBACK' AND candidate_result_sha256 IS NULL AND source_event_id IS NOT NULL AND reason_code='manual_rollback'))
		) STRICT;
		INSERT INTO strategy_selection_events_v19(sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,record_sha256,record_json,recorded_at)
		SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,record_sha256,record_json,recorded_at FROM strategy_selection_events;
		DROP TABLE paper_performance_policy_events;
		PRAGMA legacy_alter_table=ON;
		ALTER TABLE execution_authority_events RENAME TO execution_authority_events_v20_old;
		ALTER TABLE strategy_selection_events RENAME TO strategy_selection_events_v20_old;
		ALTER TABLE execution_authority_events_v19 RENAME TO execution_authority_events;
		ALTER TABLE strategy_selection_events_v19 RENAME TO strategy_selection_events;
		DROP TABLE execution_authority_events_v20_old;
		DROP TABLE strategy_selection_events_v20_old;
		PRAGMA legacy_alter_table=OFF;
		CREATE INDEX execution_authority_latest_idx ON execution_authority_events(account_ref,sequence DESC);
		CREATE TRIGGER execution_authority_events_no_update BEFORE UPDATE ON execution_authority_events BEGIN SELECT RAISE(ABORT,'execution_authority_events is insert-only'); END;
		CREATE TRIGGER execution_authority_events_no_delete BEFORE DELETE ON execution_authority_events BEGIN SELECT RAISE(ABORT,'execution_authority_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_no_update BEFORE UPDATE ON strategy_selection_events BEGIN SELECT RAISE(ABORT,'strategy_selection_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_no_delete BEFORE DELETE ON strategy_selection_events BEGIN SELECT RAISE(ABORT,'strategy_selection_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_state_guard BEFORE INSERT ON strategy_selection_events BEGIN
			SELECT CASE WHEN NEW.expected_current_event_id != COALESCE((SELECT event_id FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1),'no_event') THEN RAISE(ABORT,'strategy selection expected current event is stale') END;
			SELECT CASE WHEN NEW.previous_selected_result_sha256 != COALESCE((SELECT selected_result_sha256 FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1),'no_strategy') THEN RAISE(ABORT,'strategy selection previous result is stale') END;
			SELECT CASE WHEN NEW.event_type='SELECT' AND (NEW.selected_result_sha256 != NEW.candidate_result_sha256 OR NOT EXISTS(SELECT 1 FROM strategy_research_evidence WHERE result_sha256=NEW.candidate_result_sha256 AND target='paper_candidate')) THEN RAISE(ABORT,'strategy selection requires paper_candidate evidence') END;
			SELECT CASE WHEN NEW.event_type='ROLLBACK' AND NEW.source_event_id != COALESCE((SELECT event_id FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1),'no_event') THEN RAISE(ABORT,'strategy rollback source is stale') END;
			SELECT CASE WHEN NEW.paper_evaluation_sequence != COALESCE((SELECT MAX(sequence) FROM paper_evaluation_events),0) THEN RAISE(ABORT,'strategy selection paper evaluation sequence is stale') END;
		END;
		DELETE FROM schema_migrations WHERE version=20;`)
	if restoreErr := restoreForeignKeysForTest(db); err != nil || restoreErr != nil {
		t.Fatal(errors.Join(err, restoreErr))
	}
}

func restoreForeignKeysForTest(db *sql.DB) error {
	_, err := db.Exec(`PRAGMA foreign_keys=ON`)
	return err
}

type g38ECounts struct{ Policy, Authority, Selection int }

func g38EJournalCounts(t testing.TB, svc *Service) g38ECounts {
	t.Helper()
	var result g38ECounts
	for query, destination := range map[string]*int{
		`SELECT COUNT(*) FROM paper_performance_policy_events`: &result.Policy,
		`SELECT COUNT(*) FROM execution_authority_events`:      &result.Authority,
		`SELECT COUNT(*) FROM strategy_selection_events`:       &result.Selection,
	} {
		if err := svc.db.QueryRow(query).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func g38EPerformanceWindow(t *testing.T, closes []string) (*Service, *PaperStrategyPerformanceEvent) {
	t.Helper()
	svc, _ := testService(t, nil, nil)
	ctx := context.Background()
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:00Z") }
	artifact := rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) {
		result["experiment_id"] = "g38e-policy-window"
		result["execution"].(map[string]any)["starting_cash"] = "1000"
	})
	evidence, err := svc.registerStrategyEvidence(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.selectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.openPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	signalBar, err := svc.recordPaperMarketBar(ctx, g38c2PaperMarketBar("g38e-policy-signal", "2026-01-09T00:00:00Z", "2026-01-09T06:30:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	signal := PaperSignal{
		SchemaVersion: capitalizedPaperSignalSchema, SignalID: "g38e-policy-signal", SignalBarObservationID: signalBar.ObservationID,
		StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID,
		DataSHA256: signalBar.InputDataSHA256, Symbol: signalBar.Symbol, TargetQuantity: "2", DataAsOf: signalBar.CloseAt,
		GeneratedAt: signalBar.SourceAvailableAt, ExpiresAt: "2026-01-20T00:00:00.000000000Z",
	}
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, order, err := svc.admitPaperSignal(ctx, k2aAccountRef, signal, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	recordG38C2FillBar(t, svc, "g38e-policy-fill", "100", "5", "2026-01-10")
	if state, err := svc.runPaperOrder(ctx, order.OrderID, lease.FencingToken); err != nil || state.Status != "FILLED" {
		t.Fatalf("filled state=%+v err=%v", state, err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-15T07:00:00Z") }
	var latest *PaperStrategyPerformanceEvent
	for index, close := range closes {
		asOf := time.Date(2026, time.January, 11+index, 6, 30, 0, 0, time.UTC).Format(canonicalPaperTimeLayout)
		recordG38C3MarkBar(t, svc, "005930", "g38e-policy-mark-"+close+time.Date(2026, time.January, 11+index, 0, 0, 0, 0, time.UTC).Format("20060102"), asOf, close)
		point, err := svc.evaluatePaperPerformance(ctx, k2aAccountRef, asOf)
		if err != nil {
			t.Fatal(err)
		}
		latest, err = svc.evaluatePaperStrategyPerformance(ctx, k2aAccountRef, point.StrategySelectionEventID, point.PerformanceID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if latest == nil {
		t.Fatal("G3.8E policy fixture needs at least one strategy performance point")
	}
	return svc, latest
}

func TestG38EMigration20RejectsV19ForeignKeyInventoryDriftBeforeCommit(t *testing.T) {
	svc, _ := g38EPerformanceWindow(t, []string{"100", "100"})
	downgradePaperPerformancePolicyForTest(t, svc.db)
	driftSQLiteTableSQLForTest(t, svc.db, "strategy_selection_events",
		"source_event_id TEXT REFERENCES strategy_selection_events(event_id)",
		"source_event_id TEXT REFERENCES strategy_research_evidence(result_sha256)")
	var path string
	if err := svc.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := openExistingDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err == nil {
		t.Fatal("migration 020 accepted a v19 foreign-key inventory it did not preserve")
	}
}
