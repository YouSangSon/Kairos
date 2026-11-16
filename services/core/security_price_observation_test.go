package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func securityPriceInput() SecurityPriceObservationInput {
	return SecurityPriceObservationInput{
		Source: "local_fixture", SourceObservationID: "aapl_close_20260110", InstrumentID: "US0378331005",
		Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "250.25",
		PriceAdjustment: marketDataAdjustmentUnspecified, ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z",
	}
}

func TestLegacyV12KiwoomPriceRemainsReplayableButUnowned(t *testing.T) {
	svc, _ := testService(t, []time.Time{mustTime("2026-08-24T01:30:04Z")}, nil)
	downgradePaperMarketSignalsForTest(t, svc.db)
	downgradePaperEvaluationForTest(t, svc.db)
	if _, err := svc.db.Exec(`DROP TRIGGER instrument_listing_events_no_update;
		DROP TRIGGER instrument_listing_events_no_delete;
		DROP TRIGGER instrument_listing_events_state_guard;
		DROP TABLE instrument_listing_events;
		DELETE FROM schema_migrations WHERE version=13`); err != nil {
		t.Fatal(err)
	}
	input := SecurityPriceObservationInput{
		Source: "kiwoom_production", InstrumentID: "krx_005930", Symbol: "005930", Venue: "XKRX", Currency: "KRW",
		Price: "1250", PriceAdjustment: marketDataAdjustmentUnspecified,
		ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z",
	}
	input.SourceObservationID = "2e521ea48a32a9fb217624ca8df0771752825f14fa37c3a6539ea83a2f239b54"
	legacy := SecurityPriceObservation{
		ObservationID: "security_price_observation_legacy", Source: input.Source, SourceObservationID: input.SourceObservationID,
		InstrumentID: input.InstrumentID, Symbol: input.Symbol, Venue: input.Venue, Currency: input.Currency, Price: input.Price,
		PriceAdjustment: input.PriceAdjustment, ObservedAt: input.ObservedAt, FetchedAt: input.FetchedAt,
		RecordedAt: "2026-08-24T01:30:02Z",
	}
	_, recordSHA256, err := orderJSONHash(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO security_price_observations(
		observation_id,source,source_observation_id,instrument_id,symbol,venue,currency,price,price_adjustment,observed_at,fetched_at,record_sha256,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, legacy.ObservationID, legacy.Source, legacy.SourceObservationID,
		legacy.InstrumentID, legacy.Symbol, legacy.Venue, legacy.Currency, legacy.Price, legacy.PriceAdjustment,
		legacy.ObservedAt, legacy.FetchedAt, recordSHA256, legacy.RecordedAt); err != nil {
		t.Fatal(err)
	}
	if err := migrate(svc.db); err != nil {
		t.Fatal(err)
	}
	proof, err := marketRecovery(context.Background(), svc.db, latestSchema)
	if err != nil {
		t.Fatal(err)
	}
	var instrumentID string
	if err := svc.db.QueryRow(`SELECT instrument_id FROM security_price_observations WHERE observation_id=?`, legacy.ObservationID).Scan(&instrumentID); err != nil || proof.PriceObservations != 1 || instrumentID != "krx_005930" {
		t.Fatalf("legacy v12 price was not preserved: observations=%d instrument=%s err=%v", proof.PriceObservations, instrumentID, err)
	}
	if listing, err := svc.Market.Observations.ResolveListing(context.Background(), "XKRX", "005930", "KRW"); err == nil || listing != nil {
		t.Fatalf("legacy price inferred listing ownership: listing=%+v err=%v", listing, err)
	}
	replayed, err := svc.recordSecurityPriceObservation(context.Background(), input)
	if err != nil || replayed.ObservationID != legacy.ObservationID {
		t.Fatalf("exact legacy replay drifted: observation=%+v err=%v", replayed, err)
	}
	newInput := input
	newInput.Price = "1251"
	newInput.ObservedAt = "2026-08-24T01:30:02Z"
	newInput.FetchedAt = "2026-08-24T01:30:03Z"
	newInput.SourceObservationID = "1cd30a1ea42eb2902f3952401d6f2145d2731a3a3a308db29556bb78c1645d5b"
	if observation, err := svc.recordSecurityPriceObservation(context.Background(), newInput); err == nil || observation != nil {
		t.Fatalf("legacy identity gained write authority: observation=%+v err=%v", observation, err)
	}
}

func TestSecurityPriceObservationRestoreRejectsWeakAdjustmentConstraint(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	golden := writeCurrentSnapshot(t, svc.db)
	backup := filepath.Join(t.TempDir(), "security-prices-weak-adjustment.db")
	if _, err := createBackup(svc.db, backup, golden, backup+".manifest.json", svc.now, svc.id); err != nil {
		t.Fatal(err)
	}
	candidate, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`PRAGMA writable_schema=ON`); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`UPDATE sqlite_master SET sql=replace(sql, "price_adjustment = 'unspecified'", "price_adjustment <> ''") WHERE type='table' AND name='security_price_observations'`); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`PRAGMA writable_schema=OFF`); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(backup, golden); err == nil {
		t.Fatal("restore accepted security prices without the pinned price_adjustment constraint")
	}
}

func TestSecurityPriceObservationBackupProofAndLegacyCopyMigrations(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	if _, err := svc.recordSecurityPriceObservation(context.Background(), securityPriceInput()); err != nil {
		t.Fatal(err)
	}
	golden := writeCurrentSnapshot(t, svc.db)
	backup := filepath.Join(t.TempDir(), "security-prices.db")
	manifestPath := backup + ".manifest.json"
	manifest, err := createBackup(svc.db, backup, golden, manifestPath, svc.now, svc.id)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != backupFormat || manifest.SchemaVersion != backupSchema ||
		manifest.SecurityPriceObservationCount != 1 || len(manifest.SecurityPriceObservationStateSHA256) != 64 ||
		manifest.VerificationReceipt.SecurityPriceObservationCheck != "ok" ||
		manifest.VerificationReceipt.CandidateSecurityPriceObservationStateSHA256 != manifest.SecurityPriceObservationStateSHA256 {
		t.Fatalf("backup omitted security price proof: %+v", manifest)
	}
	if err := verifyManifest(backup, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(backup, golden); err != nil {
		t.Fatal(err)
	}
	candidate, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`DROP TRIGGER security_price_observations_no_delete`); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(backup, golden); err == nil {
		t.Fatal("restore accepted a security price table without its insert-only trigger")
	}
	indexBackup := filepath.Join(t.TempDir(), "security-prices-missing-index.db")
	if _, err := createBackup(svc.db, indexBackup, golden, indexBackup+".manifest.json", svc.now, svc.id); err != nil {
		t.Fatal(err)
	}
	candidate, err = openExistingDB(indexBackup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`DROP INDEX security_price_observations_latest_idx`); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(indexBackup, golden); err == nil {
		t.Fatal("restore accepted security prices without the exact latest index")
	}

	downgradePaperMarketSignalsForTest(t, svc.db)
	downgradePaperEvaluationForTest(t, svc.db)
	if _, err := svc.db.Exec(`DROP TRIGGER instrument_listing_events_no_update; DROP TRIGGER instrument_listing_events_no_delete; DROP TRIGGER instrument_listing_events_state_guard; DROP TABLE instrument_listing_events; DELETE FROM schema_migrations WHERE version=13; DROP TRIGGER security_price_observations_no_update; DROP TRIGGER security_price_observations_no_delete; DROP INDEX security_price_observations_latest_idx; ALTER TABLE security_price_observations RENAME TO security_price_observations_v12`); err != nil {
		t.Fatal(err)
	}
	v11Migration, err := migrationFiles.ReadFile("migrations/011_security_price_observations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(string(v11Migration)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO security_price_observations SELECT * FROM security_price_observations_v12; DROP TABLE security_price_observations_v12; DELETE FROM schema_migrations WHERE version=12`); err != nil {
		t.Fatal(err)
	}
	legacyV11Backup := filepath.Join(t.TempDir(), "legacy-v11.db")
	if _, err := svc.db.Exec(`VACUUM INTO '` + strings.ReplaceAll(legacyV11Backup, "'", "''") + `'`); err != nil {
		t.Fatal(err)
	}
	legacyV11Manifest := readJSONMap(t, manifestPath)
	legacyV11Manifest["format_version"] = "omni-folio-backup.v7"
	legacyV11Manifest["schema_version"] = "omni-folio.sqlite.v11"
	stripBenchmarkProofForLegacy(legacyV11Manifest)
	delete(legacyV11Manifest, "paper_evaluation_event_count")
	delete(legacyV11Manifest, "paper_accounting_state_sha256")
	delete(legacyV11Manifest, "paper_accounting_session_count")
	delete(legacyV11Manifest, "paper_market_bar_observation_count")
	delete(legacyV11Manifest, "paper_signal_event_count")
	delete(legacyV11Manifest, "paper_execution_authorization_count")
	delete(legacyV11Manifest, "paper_capitalized_fill_count")
	delete(legacyV11Manifest, "instrument_listing_state_sha256")
	delete(legacyV11Manifest, "instrument_listing_event_count")
	delete(legacyV11Manifest, "active_instrument_listing_count")
	legacyV11Receipt := legacyV11Manifest["verification_receipt"].(map[string]any)
	delete(legacyV11Receipt, "candidate_paper_accounting_state_sha256")
	delete(legacyV11Receipt, "instrument_listing_check")
	delete(legacyV11Receipt, "candidate_instrument_listing_state_sha256")
	v11SHA, v11Size, err := hashFile(legacyV11Backup)
	if err != nil {
		t.Fatal(err)
	}
	legacyV11Manifest["db_sha256"] = v11SHA
	legacyV11Manifest["size_bytes"] = v11Size
	legacyV11Manifest["verification_receipt"].(map[string]any)["candidate_db_sha256"] = v11SHA
	legacyV11ManifestPath := filepath.Join(t.TempDir(), "legacy-v11.manifest.json")
	legacyReplayGoldenV11 := legacyGoldenForTest(t, golden, legacyV11Manifest)
	writeJSONFile(t, legacyV11ManifestPath, legacyV11Manifest)
	beforeV11SHA, beforeV11Size, err := hashFile(legacyV11Backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(legacyV11Backup, legacyReplayGoldenV11, legacyV11ManifestPath); err != nil {
		t.Fatal(err)
	}
	afterV11SHA, afterV11Size, err := hashFile(legacyV11Backup)
	if err != nil || beforeV11SHA != afterV11SHA || beforeV11Size != afterV11Size {
		t.Fatalf("legacy v11 source was changed during copy migration: before=(%s,%d) after=(%s,%d) err=%v", beforeV11SHA, beforeV11Size, afterV11SHA, afterV11Size, err)
	}

	if _, err := svc.db.Exec(`DROP TRIGGER security_price_observations_no_update; DROP TRIGGER security_price_observations_no_delete; DROP TABLE security_price_observations; DELETE FROM schema_migrations WHERE version IN (11,12)`); err != nil {
		t.Fatal(err)
	}
	legacyBackup := filepath.Join(t.TempDir(), "legacy-v10.db")
	if _, err := svc.db.Exec(`VACUUM INTO '` + strings.ReplaceAll(legacyBackup, "'", "''") + `'`); err != nil {
		t.Fatal(err)
	}
	legacyManifest := readJSONMap(t, manifestPath)
	legacyManifest["format_version"] = "omni-folio-backup.v6"
	legacyManifest["schema_version"] = "omni-folio.sqlite.v10"
	stripBenchmarkProofForLegacy(legacyManifest)
	delete(legacyManifest, "paper_evaluation_event_count")
	delete(legacyManifest, "paper_accounting_state_sha256")
	delete(legacyManifest, "paper_accounting_session_count")
	delete(legacyManifest, "paper_market_bar_observation_count")
	delete(legacyManifest, "paper_signal_event_count")
	delete(legacyManifest, "paper_execution_authorization_count")
	delete(legacyManifest, "paper_capitalized_fill_count")
	delete(legacyManifest, "security_price_observation_state_sha256")
	delete(legacyManifest, "security_price_observation_count")
	delete(legacyManifest, "instrument_listing_state_sha256")
	delete(legacyManifest, "instrument_listing_event_count")
	delete(legacyManifest, "active_instrument_listing_count")
	receipt := legacyManifest["verification_receipt"].(map[string]any)
	delete(receipt, "candidate_paper_accounting_state_sha256")
	delete(receipt, "security_price_observation_check")
	delete(receipt, "candidate_security_price_observation_state_sha256")
	delete(receipt, "instrument_listing_check")
	delete(receipt, "candidate_instrument_listing_state_sha256")
	sha, size, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	legacyManifest["db_sha256"] = sha
	legacyManifest["size_bytes"] = size
	legacyManifest["verification_receipt"].(map[string]any)["candidate_db_sha256"] = sha
	legacyManifestPath := filepath.Join(t.TempDir(), "legacy-v10.manifest.json")
	legacyReplayGoldenV10 := legacyGoldenForTest(t, golden, legacyManifest)
	writeJSONFile(t, legacyManifestPath, legacyManifest)
	beforeSHA, beforeSize, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(legacyBackup, legacyReplayGoldenV10, legacyManifestPath); err != nil {
		t.Fatal(err)
	}
	mixed := readJSONMap(t, legacyManifestPath)
	mixed["security_price_observation_state_sha256"] = strings.Repeat("0", 64)
	mixed["security_price_observation_count"] = float64(0)
	mixedReceipt := mixed["verification_receipt"].(map[string]any)
	mixedReceipt["security_price_observation_check"] = "ok"
	mixedReceipt["candidate_security_price_observation_state_sha256"] = strings.Repeat("0", 64)
	mixedPath := filepath.Join(t.TempDir(), "mixed-v6-v7.manifest.json")
	writeJSONFile(t, mixedPath, mixed)
	if err := verifyManifest(legacyBackup, golden, mixedPath); err == nil {
		t.Fatal("legacy v6 manifest with v7-only security price fields was accepted")
	}
	afterSHA, afterSize, err := hashFile(legacyBackup)
	if err != nil || beforeSHA != afterSHA || beforeSize != afterSize {
		t.Fatalf("legacy v10 source was changed during copy migration: before=(%s,%d) after=(%s,%d) err=%v", beforeSHA, beforeSize, afterSHA, afterSize, err)
	}
	var version int
	if err := svc.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 10 {
		t.Fatalf("legacy v10 source was migrated in place: version=%d err=%v", version, err)
	}
}
