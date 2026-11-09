package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ledger "omni-folio/services/core/internal/modules/ledger"
	ledgercontracts "omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/application"
	market "omni-folio/services/core/internal/modules/marketdata"
	trading "omni-folio/services/core/internal/modules/trading"
	watch "omni-folio/services/core/internal/modules/watchlist"
	"omni-folio/services/core/internal/storage/observation"
)

func backupStore(t testing.TB, db *sql.DB) *Store {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC) }
	serial := 0
	id := func(prefix string) string { serial++; return fmt.Sprintf("%s_%d", prefix, serial) }
	factory := func(candidate *sql.DB) (Owners, error) {
		registry := observation.New(candidate, time.Now)
		ledgerAPI, err := ledger.Compose(ledger.Config{DB: candidate, Observations: registry, Now: now, ID: id, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
		if err != nil {
			return Owners{}, err
		}
		marketModule, err := market.Compose(market.Config{DB: candidate, Observations: registry, Ledger: ledgerAPI, Now: now, ID: id})
		if err != nil {
			return Owners{}, err
		}
		tradingAPI, err := trading.Compose(trading.Config{DB: candidate, Observations: registry, Clock: now, ID: id, ExecutionOwner: "backup-execution", RunnerOwner: "backup-runner"})
		if err != nil {
			return Owners{}, err
		}
		watchModule, err := watch.Compose(candidate, registry, nil, now)
		if err != nil {
			return Owners{}, err
		}
		return Owners{Observations: registry, Ledger: ledgerAPI, Market: marketModule.Recovery, Trading: tradingAPI, Watchlist: watchModule.Recovery}, nil
	}
	owners, err := factory(db)
	if err != nil {
		t.Fatal(err)
	}
	return New(db, owners.Observations, owners.Trading, now, id, factory)
}

func seedBackupLedger(t *testing.T, store *Store) ledgercontracts.PortfolioSnapshot {
	t.Helper()
	owners, err := store.owners(store.db)
	if err != nil {
		t.Fatal(err)
	}
	api := owners.Ledger.(ledgercontracts.Service)
	raw := []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount\ndeposit,account-main,DEPOSIT,2026-01-01T00:00:00Z,,,,,USD,100\nbuy,account-main,BUY,2026-01-02T00:00:00Z,AAPL,1,10,0,USD,-10\n")
	preview, issue := api.PreviewImport(context.Background(), raw)
	if issue != nil {
		t.Fatal(issue)
	}
	if _, issue := api.ApplyImport(context.Background(), ledgercontracts.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: "backup"}); issue != nil {
		t.Fatal(issue)
	}
	snapshot, err := api.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return *snapshot
}
func writeBackupJSON(t testing.TB, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupV21UsesActualOwnerProofsAndRejectsTampering(t *testing.T) {
	db := migrationDB(t, 0)
	if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := backupStore(t, db)
	snapshot := seedBackupLedger(t, store)
	dir := t.TempDir()
	golden := filepath.Join(dir, "golden.json")
	candidate := filepath.Join(dir, "candidate.db")
	manifestPath := filepath.Join(dir, "manifest.json")
	writeBackupJSON(t, golden, snapshot)
	manifest, err := store.CreateBackup(context.Background(), contracts.BackupRequest{OutputPath: candidate, GoldenPath: golden, ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != "omni-folio-backup.v21" || manifest.SchemaVersion != "omni-folio.sqlite.v27" || manifest.SourceLedgerRevision != snapshot.LedgerRevision || manifest.VerificationReceipt.Status != "verified" || !manifest.VerificationReceipt.EligibleForActivation {
		t.Fatalf("backup contract changed: %+v", manifest)
	}
	if err := store.VerifyManifest(context.Background(), candidate, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyRestore(context.Background(), candidate, golden); err != nil {
		t.Fatal(err)
	}
	manifest.SourceLedgerRevision = "rev_9999999999"
	writeBackupJSON(t, manifestPath, manifest)
	if err := store.VerifyManifest(context.Background(), candidate, golden, manifestPath); err == nil || !strings.Contains(err.Error(), "backup ledger revision mismatch") {
		t.Fatalf("tampered revision accepted: %v", err)
	}
}

func TestBackupGoldenMismatchDiscardsOwnedCandidate(t *testing.T) {
	db := migrationDB(t, 0)
	if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := backupStore(t, db)
	snapshot := seedBackupLedger(t, store)
	snapshot.LedgerRevision = "rev_9999999999"
	dir := t.TempDir()
	golden := filepath.Join(dir, "wrong-golden.json")
	candidate := filepath.Join(dir, "candidate.db")
	manifest := filepath.Join(dir, "manifest.json")
	writeBackupJSON(t, golden, snapshot)
	if _, err := store.CreateBackup(context.Background(), contracts.BackupRequest{OutputPath: candidate, GoldenPath: golden, ManifestPath: manifest}); err == nil {
		t.Fatal("wrong golden was accepted")
	}
	for _, path := range []string{candidate, manifest} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed backup retained %s: %v", path, err)
		}
	}
}

func TestLegacyV11ManifestMigratesOnlyOwnedCandidate(t *testing.T) {
	db := migrationDB(t, 17)
	store := backupStore(t, db)
	var source string
	if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	beforeSHA, beforeSize, err := hashFile(source)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := store.readProofs(context.Background(), db, 17, true, false)
	if err != nil {
		t.Fatal(err)
	}
	facts.Ledger.Snapshot = ledgercontracts.PortfolioSnapshot{PortfolioID: "portfolio_main", LedgerRevision: "rev_0000000000", AsOf: "1970-01-01T00:00:00Z", RecordedAt: "1970-01-01T00:00:00Z", ValuationStatus: "unavailable", Cash: []ledgercontracts.Money{}, Holdings: []ledgercontracts.Holding{}, RealizedPnL: []ledgercontracts.Money{}, Provenance: ledgercontracts.Provenance{EventIDs: []string{}, ReceiptIDs: []string{}}}
	dir := t.TempDir()
	golden := filepath.Join(dir, "legacy-golden.json")
	manifestPath := filepath.Join(dir, "legacy-manifest.json")
	writeBackupJSON(t, golden, facts.Ledger.Snapshot)
	goldenSHA, _, err := hashFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := application.BuildManifest(facts, facts, store.now(), store.now(), beforeSHA, beforeSize, goldenSHA, store.id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["format_version"] = "omni-folio-backup.v11"
	fields["schema_version"] = "omni-folio.sqlite.v17"
	receipt := fields["verification_receipt"].(map[string]any)
	for _, prefix := range []string{"paper_performance_", "paper_strategy_performance_", "paper_runner_", "benchmark_series_", "owner_fx_series_", "owner_price_series_", "watchlist_", "paper_promotion_"} {
		for key := range fields {
			if strings.HasPrefix(key, prefix) {
				delete(fields, key)
			}
		}
		for key := range receipt {
			if strings.HasPrefix(key, prefix) || strings.HasPrefix(key, "candidate_"+prefix) {
				delete(receipt, key)
			}
		}
	}
	delete(fields, "active_paper_runner_lease_count")
	writeBackupJSON(t, manifestPath, fields)
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	if err := store.VerifyManifest(context.Background(), source, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	afterSHA, afterSize, err := hashFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if beforeSHA != afterSHA || beforeSize != afterSize {
		t.Fatal("legacy verification changed source artifact")
	}
	var schema, replay int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&schema); err != nil || schema != 17 {
		t.Fatal("legacy source history changed", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='ledger_replay_state'`).Scan(&replay); err != nil || replay != 0 {
		t.Fatal("legacy source gained replay state", err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("owned staging candidate retained: %v %v", entries, err)
	}
}

func TestStartupPreservesSubsetAndFailurePrefix(t *testing.T) {
	db := migrationDB(t, 0)
	if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := backupStore(t, db)
	if _, err := db.Exec(`UPDATE ledger_meta SET recorded_at='invalid' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireStartup(context.Background()); err != nil {
		t.Fatalf("startup added full ledger snapshot proof: %v", err)
	}
	if _, err := store.readProofs(context.Background(), db, 27, false, true); err == nil {
		t.Fatal("snapshot fixture did not fail full proof")
	}
	if _, err := db.Exec(`DROP TRIGGER watchlist_state_update_guard`); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireStartup(context.Background()); err == nil || !strings.Contains(err.Error(), "watchlist startup recovery") {
		t.Fatalf("startup failure prefix changed: %v", err)
	}
}

func TestManifestRejectsLedgerChangeBetweenSourceAndCandidate(t *testing.T) {
	db := migrationDB(t, 0)
	if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := backupStore(t, db)
	seedBackupLedger(t, store)
	source, err := store.readProofs(context.Background(), db, 27, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ledger_meta SET recorded_at='2026-01-10T15:00:00.1Z',last_verified_at='2026-01-10T15:00:00.1Z' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	candidate, err := store.readProofs(context.Background(), db, 27, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if source.Ledger.Snapshot.LedgerRevision != candidate.Ledger.Snapshot.LedgerRevision {
		t.Fatal("fixture changed ledger revision")
	}
	if _, err := application.BuildManifest(source, candidate, store.now(), store.now(), "db", 0, "snapshot", store.id); err == nil || !strings.Contains(err.Error(), "backup ledger recovery proof does not match source") {
		t.Fatalf("changed source observation was accepted: %v", err)
	}
}
