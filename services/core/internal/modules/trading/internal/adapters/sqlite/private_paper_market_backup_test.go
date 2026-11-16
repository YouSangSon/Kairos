package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	ledgercontracts "omni-folio/services/core/internal/modules/ledger/contracts"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestG38C2PaperMarketBackupV11AndV10OwnedCopyMigration(t *testing.T) {
	ctx := context.Background()
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	if _, err := recordG38C2PaperSignalForTest(svc, k2aAccountRef, signal); err != nil {
		t.Fatal(err)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "paper-market-v11.db")
	manifestPath := backup + ".manifest.json"
	manifest, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(ctx, maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != maintenancecontracts.BackupFormat || manifest.SchemaVersion != maintenancecontracts.BackupSchema ||
		manifest.PaperAccountingSessionCount != 1 || manifest.PaperMarketBarObservationCount != 1 || manifest.PaperSignalEventCount != 1 ||
		manifest.PaperExecutionAuthorizationCount != 0 || manifest.PaperCapitalizedFillCount != 0 ||
		manifest.PaperAccountingStateSHA256 == "" || manifest.VerificationReceipt.CandidatePaperAccountingStateSHA256 != manifest.PaperAccountingStateSHA256 {
		t.Fatalf("v11 manifest omitted paper market proof: %+v", manifest)
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(ctx, backup, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"paper_market_bar_observation_count", "paper_signal_event_count",
		"paper_execution_authorization_count", "paper_capitalized_fill_count",
	} {
		tampered := readJSONMap(t, manifestPath)
		delete(tampered, field)
		path := filepath.Join(t.TempDir(), "missing-"+field+".manifest.json")
		writeJSONFile(t, path, tampered)
		if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(ctx, backup, golden, path); err == nil {
			t.Fatalf("v11 manifest without %s was accepted", field)
		}
	}

	legacySvc, _ := testService(t, nil, nil)
	legacyEvidence, legacySelection := selectedPaperStrategy(t, legacySvc)
	if _, err := legacySvc.OpenPaperAccountingSession(ctx, k2aAccountRef, legacyEvidence.ResultSHA256, legacySelection.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	legacySignal := paperEvaluationSignal(legacyEvidence.ResultSHA256, legacySelection.CurrentEventID, "g38c2-v10-legacy-order")
	downgradePaperAuthorizationForTest(t, legacySvc.db.DB)
	insertG38C2LegacyPaperOrder(t, legacySvc, legacySignal)
	if err := privateLegacyMigrate(legacySvc); err != nil {
		t.Fatal(err)
	}
	legacyOrderProof, err := proveOrderRecovery(ctx, legacySvc.db)
	if err != nil {
		t.Fatal(err)
	}
	legacyPaperProof, err := proveLegacyPaperAccountingRecovery(ctx, legacySvc.db)
	if err != nil {
		t.Fatal(err)
	}
	legacyGolden := mixedWriteCurrentSnapshot(t, legacySvc)
	currentBackup := filepath.Join(t.TempDir(), "paper-market-before-v10-downgrade.db")
	currentManifestPath := currentBackup + ".manifest.json"
	if _, err := mixedBackupModule(t, legacySvc, legacySvc.now, legacySvc.id).CreateBackup(ctx, maintenancecontracts.BackupRequest{OutputPath: currentBackup, GoldenPath: legacyGolden, ManifestPath: currentManifestPath}); err != nil {
		t.Fatal(err)
	}
	downgradePaperMarketSignalsForTest(t, legacySvc.db.DB)
	legacyBackup := filepath.Join(t.TempDir(), "paper-market-v10.db")
	if _, err := legacySvc.db.Exec(`VACUUM INTO '` + strings.ReplaceAll(legacyBackup, "'", "''") + `'`); err != nil {
		t.Fatal(err)
	}
	legacyManifest := readJSONMap(t, currentManifestPath)
	legacyManifest["format_version"] = "omni-folio-backup.v10"
	legacyManifest["schema_version"] = "omni-folio.sqlite.v15"
	stripBenchmarkProofForLegacy(legacyManifest)
	legacyManifest["paper_accounting_state_sha256"] = legacyPaperProof.SHA256
	legacyManifest["paper_accounting_session_count"] = legacyPaperProof.Sessions
	delete(legacyManifest, "paper_market_bar_observation_count")
	delete(legacyManifest, "paper_signal_event_count")
	delete(legacyManifest, "paper_execution_authorization_count")
	delete(legacyManifest, "paper_capitalized_fill_count")
	legacyManifest["verification_receipt"].(map[string]any)["candidate_paper_accounting_state_sha256"] = legacyPaperProof.SHA256
	legacySHA, legacySize, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	legacyManifest["db_sha256"], legacyManifest["size_bytes"] = legacySHA, legacySize
	legacyManifest["verification_receipt"].(map[string]any)["candidate_db_sha256"] = legacySHA
	legacyManifestPath := filepath.Join(t.TempDir(), "paper-market-v10.manifest.json")
	legacyReplayGolden := paperMarketLegacyGoldenForTest(t, legacyGolden, legacyManifest)
	writeJSONFile(t, legacyManifestPath, legacyManifest)
	beforeSHA, beforeSize, err := hashFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	if err := mixedBackupModule(t, legacySvc, legacySvc.now, legacySvc.id).VerifyManifest(ctx, legacyBackup, legacyReplayGolden, legacyManifestPath); err != nil {
		t.Fatal(err)
	}
	afterSHA, afterSize, err := hashFile(legacyBackup)
	if err != nil || beforeSHA != afterSHA || beforeSize != afterSize {
		t.Fatalf("v10 source changed during owned-copy migration: before=(%s,%d) after=(%s,%d) err=%v", beforeSHA, beforeSize, afterSHA, afterSize, err)
	}
	ownedCopy := filepath.Join(t.TempDir(), "paper-market-v10-owned.db")
	if err := copyPaperMarketBackupForTest(legacyBackup, ownedCopy); err != nil {
		t.Fatal(err)
	}
	candidate, err := openExistingDB(ownedCopy)
	if err != nil {
		t.Fatal(err)
	}
	if err := privateLegacyMigrate(newService(candidate, legacySvc.now, legacySvc.id)); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	ownedOrders, orderErr := proveOrderRecovery(ctx, &database{candidate})
	ownedPaper, paperErr := provePaperAccountingRecovery(ctx, &database{candidate})
	closeErr := candidate.Close()
	if orderErr != nil || paperErr != nil || closeErr != nil || ownedOrders != legacyOrderProof || ownedPaper.Sessions != 1 ||
		ownedPaper.MarketBars != 0 || ownedPaper.Signals != 0 || ownedPaper.Authorizations != 0 || ownedPaper.CapitalizedFills != 0 {
		t.Fatalf("v10 owned proof orders=%+v paper=%+v errors=(%v,%v,%v)", ownedOrders, ownedPaper, orderErr, paperErr, closeErr)
	}
}

func downgradePaperMarketSignalsForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	downgradePaperAuthorizationForTest(t, db)
	if _, err := db.Exec(`DROP TRIGGER order_idempotency_legacy_paper_signal_guard;
		DROP TRIGGER paper_signal_events_no_update;
		DROP TRIGGER paper_signal_events_no_delete;
		DROP TRIGGER paper_signal_events_state_guard;
		DROP TABLE paper_signal_events;
		DROP TRIGGER paper_market_bar_observations_no_update;
		DROP TRIGGER paper_market_bar_observations_no_delete;
		DROP TABLE paper_market_bar_observations;
		DELETE FROM schema_migrations WHERE version=16`); err != nil {
		t.Fatal(err)
	}
}

func paperMarketLegacyGoldenForTest(t *testing.T, currentPath string, manifest map[string]any) string {
	t.Helper()
	raw, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot ledgercontracts.PortfolioSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.CostBasisPolicy = "fifo_exact_else_half_even_residual_8_v1"
	path := filepath.Join(t.TempDir(), "legacy-policy-golden.json")
	writeJSONFile(t, path, snapshot)
	sha, _, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest["expected_snapshot_sha256"] = sha
	manifest["verification_receipt"].(map[string]any)["candidate_snapshot_sha256"] = sha
	return path
}

func copyPaperMarketBackupForTest(source, target string) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0600)
}
