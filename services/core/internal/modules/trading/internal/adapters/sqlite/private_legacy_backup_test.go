package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	watchmodule "omni-folio/services/core/internal/modules/watchlist"
	watchapi "omni-folio/services/core/internal/modules/watchlist/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBenchmarkSeriesLegacyV16CannotActivateAnActiveRunnerLease(t *testing.T) {
	svc, _ := g38EPerformanceWindow(t, []string{"100"})
	if _, err := svc.acquirePaperRunnerLease(context.Background(), k2aAccountRef); err != nil {
		t.Fatal(err)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "active-v16.db")
	manifestPath := backup + ".manifest.json"
	if _, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	db, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	dropBenchmarkSchemaForLegacy(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	fields := readJSONMap(t, manifestPath)
	stripBenchmarkProofForLegacy(fields)
	fields["format_version"], fields["schema_version"] = "omni-folio-backup.v16", "omni-folio.sqlite.v22"
	sha, size, err := hashFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fields["verification_receipt"].(map[string]any)
	fields["db_sha256"], fields["size_bytes"], receipt["candidate_db_sha256"] = sha, size, sha
	// A forged activation claim must not hide the actually captured active lease.
	receipt["status"], receipt["eligible_for_activation"], receipt["errors"] = "verified", true, []string{}
	writeJSONFile(t, manifestPath, fields)
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(context.Background(), backup, golden, manifestPath); err == nil {
		t.Fatal("active v16 runner became eligible")
	}
}

func dropBenchmarkSchemaForLegacy(t testing.TB, db *sql.DB) {
	t.Helper()
	dropOwnerFXSchemaForLegacy(t, db)
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='benchmark_series_versions'`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 0 {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM benchmark_series_versions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("legacy fixture cannot discard benchmark data")
		}
		if _, err := db.Exec(`DROP TABLE benchmark_series_versions`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=23`); err != nil {
		t.Fatal(err)
	}
}

func stripBenchmarkProofForLegacy(manifest map[string]any) {
	stripOwnerFXProofForLegacy(manifest)
	for _, key := range []string{"benchmark_series_state_sha256", "benchmark_series_version_count", "benchmark_series_point_count"} {
		delete(manifest, key)
	}
	receipt := manifest["verification_receipt"].(map[string]any)
	for _, key := range []string{"benchmark_series_check", "candidate_benchmark_series_state_sha256", "candidate_benchmark_series_version_count", "candidate_benchmark_series_point_count"} {
		delete(receipt, key)
	}
}

func TestOwnerFXLegacyV17CannotHideAnActiveRunnerLease(t *testing.T) {
	svc, _ := g38EPerformanceWindow(t, []string{"100"})
	if _, err := svc.acquirePaperRunnerLease(context.Background(), k2aAccountRef); err != nil {
		t.Fatal(err)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "active-v17.db")
	manifestPath := backup + ".manifest.json"
	if _, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	db, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	dropOwnerFXSchemaForLegacy(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	fields := readJSONMap(t, manifestPath)
	stripOwnerFXProofForLegacy(fields)
	fields["format_version"], fields["schema_version"] = "omni-folio-backup.v17", "omni-folio.sqlite.v23"
	sha, size, err := hashFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fields["verification_receipt"].(map[string]any)
	fields["db_sha256"], fields["size_bytes"], receipt["candidate_db_sha256"] = sha, size, sha
	receipt["status"], receipt["eligible_for_activation"], receipt["errors"] = "verified", true, []string{}
	writeJSONFile(t, manifestPath, fields)
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(context.Background(), backup, golden, manifestPath); err == nil {
		t.Fatal("active v17 runner became eligible")
	}
}

func dropOwnerFXSchemaForLegacy(t testing.TB, db *sql.DB) {
	t.Helper()
	dropOwnerPriceSchemaForLegacy(t, db)
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='owner_fx_series_versions'`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 0 {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM owner_fx_series_versions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("legacy fixture cannot discard declared FX data")
		}
		if _, err := db.Exec(`DROP TRIGGER fx_observations_owner_pair_guard; DROP TABLE owner_fx_series_versions`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=24`); err != nil {
		t.Fatal(err)
	}
}

func stripOwnerFXProofForLegacy(manifest map[string]any) {
	stripOwnerPriceProofForLegacy(manifest)
	for _, key := range []string{"owner_fx_series_state_sha256", "owner_fx_series_version_count", "owner_fx_series_point_count"} {
		delete(manifest, key)
	}
	receipt := manifest["verification_receipt"].(map[string]any)
	for _, key := range []string{"owner_fx_series_check", "candidate_owner_fx_series_state_sha256", "candidate_owner_fx_series_version_count", "candidate_owner_fx_series_point_count"} {
		delete(receipt, key)
	}
}

func TestOwnerPriceLegacyV18CannotHideActiveLease(t *testing.T) {
	svc, _ := g38EPerformanceWindow(t, []string{"100"})
	if _, err := svc.acquirePaperRunnerLease(context.Background(), k2aAccountRef); err != nil {
		t.Fatal(err)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "active-v18.db")
	mp := backup + ".manifest.json"
	if _, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: mp}); err != nil {
		t.Fatal(err)
	}
	db, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	dropOwnerPriceSchemaForLegacy(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f := readJSONMap(t, mp)
	stripOwnerPriceProofForLegacy(f)
	f["format_version"], f["schema_version"] = "omni-folio-backup.v18", "omni-folio.sqlite.v24"
	sha, size, err := hashFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	r := f["verification_receipt"].(map[string]any)
	f["db_sha256"], f["size_bytes"], r["candidate_db_sha256"] = sha, size, sha
	r["status"], r["eligible_for_activation"], r["errors"] = "verified", true, []string{}
	writeJSONFile(t, mp, f)
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(context.Background(), backup, golden, mp); err == nil {
		t.Fatal("active v18 lease became eligible")
	}
}

func dropOwnerPriceSchemaForLegacy(t testing.TB, db *sql.DB) {
	dropWatchlistSchemaForLegacy(t, db)
	t.Helper()
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='owner_price_series_versions'`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 0 {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM owner_price_series_versions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("legacy fixture cannot discard declared prices")
		}
		if _, err := db.Exec(`DROP TRIGGER security_price_observations_owner_guard; DROP TABLE owner_price_series_versions`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=25`); err != nil {
		t.Fatal(err)
	}
}

func stripOwnerPriceProofForLegacy(manifest map[string]any) {
	stripWatchlistProofForLegacy(manifest)
	for _, key := range []string{"owner_price_series_state_sha256", "owner_price_series_version_count", "owner_price_series_point_count"} {
		delete(manifest, key)
	}
	r := manifest["verification_receipt"].(map[string]any)
	for _, key := range []string{"owner_price_series_check", "candidate_owner_price_series_state_sha256", "candidate_owner_price_series_version_count", "candidate_owner_price_series_point_count"} {
		delete(r, key)
	}
}

func TestWatchlistLegacyV19CannotHideActiveLease(t *testing.T) {
	svc, _ := g38EPerformanceWindow(t, []string{"100"})
	if _, err := svc.acquirePaperRunnerLease(context.Background(), k2aAccountRef); err != nil {
		t.Fatal(err)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "active-v19.db")
	mp := backup + ".manifest.json"
	if _, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: mp}); err != nil {
		t.Fatal(err)
	}
	db, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	dropWatchlistSchemaForLegacy(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f := readJSONMap(t, mp)
	stripWatchlistProofForLegacy(f)
	f["format_version"], f["schema_version"] = "omni-folio-backup.v19", "omni-folio.sqlite.v25"
	sha, size, err := hashFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	r := f["verification_receipt"].(map[string]any)
	f["db_sha256"], f["size_bytes"], r["candidate_db_sha256"] = sha, size, sha
	r["status"], r["eligible_for_activation"], r["errors"] = "verified", true, []string{}
	writeJSONFile(t, mp, f)
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(context.Background(), backup, golden, mp); err == nil {
		t.Fatal("active legacy runner became eligible")
	}
}

func dropWatchlistSchemaForLegacy(t testing.TB, db *sql.DB) {
	t.Helper()
	dropPaperPromotionSchemaForLegacy(t, db)
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='watchlist_state'`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 0 {
		state, _, err := watchlistStateForTest(t, context.Background(), db)
		if err != nil || state.Revision != 0 || len(state.Entries) != 0 {
			t.Fatal("legacy fixture cannot discard watchlist state", err)
		}
		if _, err := db.Exec(`DROP TABLE watchlist_state`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=26`); err != nil {
		t.Fatal(err)
	}
}

func stripWatchlistProofForLegacy(manifest map[string]any) {
	stripPaperPromotionProofForLegacy(manifest)
	for _, key := range []string{"watchlist_state_sha256", "watchlist_revision", "watchlist_entry_count"} {
		delete(manifest, key)
	}
	receipt := manifest["verification_receipt"].(map[string]any)
	for _, key := range []string{"watchlist_check", "candidate_watchlist_state_sha256", "candidate_watchlist_revision", "candidate_watchlist_entry_count"} {
		delete(receipt, key)
	}
}

func watchlistStateForTest(t testing.TB, ctx context.Context, db *sql.DB) (*watchapi.WatchlistState, watchapi.RecoveryProof, error) {
	t.Helper()
	registry := observation.New(db, time.Now)
	module, err := watchmodule.Compose(db, registry, nil, time.Now)
	if err != nil {
		return nil, watchapi.RecoveryProof{}, err
	}
	var state watchapi.WatchlistState
	var proof watchapi.RecoveryProof
	err = registry.VerifyBackup(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		var err error
		state, err = module.States.ReadState(ctx, ref)
		if err != nil {
			return err
		}
		proof, err = module.Recovery.VerifyBackup(ctx, ref)
		return err
	})
	return &state, proof, err
}

func dropPaperPromotionSchemaForLegacy(t testing.TB, db *sql.DB) {
	t.Helper()
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='paper_promotion_claims'`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present == 0 {
		return
	}
	var count int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM paper_promotion_claims) + (SELECT COUNT(*) FROM paper_promotion_decisions) +
		(SELECT COUNT(*) FROM strategy_selection_events WHERE paper_promotion_attempt_id IS NOT NULL)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("legacy fixture cannot discard paper promotion history", err)
	}
	indexes, triggers := privateSelectionSchemaForLegacy(t, db)
	script, err := migrationFiles.ReadFile("migrations/020_paper_performance_policy_events.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(script), "CREATE TABLE strategy_selection_events_new")
	if !found {
		t.Fatal("legacy selection definition missing")
	}
	table, _, found = strings.Cut(table, "INSERT INTO execution_authority_events_new")
	if !found {
		t.Fatal("legacy selection definition boundary missing")
	}
	_, guard, found := strings.Cut(string(script), "CREATE TRIGGER strategy_selection_events_state_guard")
	if !found {
		t.Fatal("legacy selection guard missing")
	}
	guard, _, found = strings.Cut(guard, "CREATE TRIGGER execution_authority_events_state_guard")
	if !found {
		t.Fatal("legacy selection guard boundary missing")
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := restoreForeignKeysForTest(db); err != nil {
			t.Fatal(err)
		}
	}()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, trigger := range triggers {
		if _, err := tx.Exec(`DROP TRIGGER "` + strings.ReplaceAll(trigger.Name, `"`, `""`) + `"`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec("CREATE TABLE strategy_selection_events_new" + table); err != nil {
		t.Fatal(err)
	}
	const columns = "sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,record_sha256,record_json,recorded_at"
	if _, err := tx.Exec(`INSERT INTO strategy_selection_events_new(` + columns + `) SELECT ` + columns + ` FROM strategy_selection_events ORDER BY sequence`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DROP TRIGGER strategy_selection_events_no_update;
		DROP TRIGGER strategy_selection_events_no_delete;
		DROP TRIGGER strategy_selection_events_state_guard;
		DROP TABLE strategy_selection_events;
		ALTER TABLE strategy_selection_events_new RENAME TO strategy_selection_events;
		DROP TABLE paper_promotion_decisions;
		DROP TABLE paper_promotion_claims;
		CREATE TRIGGER strategy_selection_events_no_update BEFORE UPDATE ON strategy_selection_events
		BEGIN SELECT RAISE(ABORT, 'strategy_selection_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_no_delete BEFORE DELETE ON strategy_selection_events
		BEGIN SELECT RAISE(ABORT, 'strategy_selection_events is insert-only'); END;
		DELETE FROM schema_migrations WHERE version=27;`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("CREATE TRIGGER strategy_selection_events_state_guard" + guard); err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if _, err := tx.Exec(index.SQL); err != nil {
			t.Fatal(err)
		}
	}
	for _, trigger := range triggers {
		if _, err := tx.Exec(trigger.SQL); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func stripPaperPromotionProofForLegacy(manifest map[string]any) {
	for _, key := range []string{"paper_promotion_state_sha256", "paper_promotion_claim_count", "paper_promotion_decision_count"} {
		delete(manifest, key)
	}
	delete(manifest["verification_receipt"].(map[string]any), "candidate_paper_promotion_state_sha256")
}

func restoreForeignKeysForTest(db *sql.DB) error {
	_, err := db.Exec(`PRAGMA foreign_keys=ON`)
	return err
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

type privateLegacySchemaObject struct{ Name, SQL string }

// Legacy fixtures preserve dependent definitions while downgrading their test database.
func privateSelectionSchemaForLegacy(t testing.TB, db *sql.DB) (indexes, triggers []privateLegacySchemaObject) {
	t.Helper()
	rows, err := db.Query(`SELECT type,name,sql FROM sqlite_master
 WHERE (type='index' AND tbl_name='strategy_selection_events' AND sql IS NOT NULL)
 OR (type='trigger' AND tbl_name NOT IN ('strategy_selection_events','paper_promotion_claims','paper_promotion_decisions') AND instr(sql,'strategy_selection_events')>0)
 ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var object privateLegacySchemaObject
		if err := rows.Scan(&kind, &object.Name, &object.SQL); err != nil {
			t.Fatal(err)
		}
		if kind == "index" {
			indexes = append(indexes, object)
		} else {
			triggers = append(triggers, object)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	return indexes, triggers
}

func TestLegacyBackupFixturePreservesSelectionDependencies(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	indexes, triggers := privateSelectionSchemaForLegacy(t, svc.db.DB)
	if len(indexes) == 0 || len(triggers) == 0 {
		t.Fatalf("selection fixture omitted dependencies: indexes=%d triggers=%d", len(indexes), len(triggers))
	}
	dropPaperPromotionSchemaForLegacy(t, svc.db.DB)
	for _, object := range append(indexes, triggers...) {
		var actual string
		if err := svc.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, object.Name).Scan(&actual); err != nil || actual != object.SQL {
			t.Fatalf("legacy fixture changed %s: SQL=%q want=%q err=%v", object.Name, actual, object.SQL, err)
		}
	}
	var enabled int
	if err := svc.db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("legacy fixture foreign keys=%d err=%v", enabled, err)
	}
}
