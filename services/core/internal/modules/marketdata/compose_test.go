package marketdata_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"omni-folio/services/core/internal/composition/observations"
	ledger "omni-folio/services/core/internal/modules/ledger"
	lc "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
)

const priceInput = `{"schema_version":"owner-price-series.v1","source_version":"price-v1","instrument_id":"instrument_aapl","symbol":"AAPL","venue":"XNAS","currency":"USD","price_adjustment":"unadjusted","sample":false,"fetched_at":"2026-01-01T00:00:00Z","points":[{"observed_at":"2026-01-01T00:00:00Z","price":"200"}]}`

func marketFixture(t *testing.T) (market.Module, lc.Service, *sql.DB, *observation.Registry) {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "market.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"001_init.sql", "003_broker_snapshots.sql", "005_ledger_events.sql", "008_cash_void.sql", "009_fx_exchange.sql", "010_fx_observations.sql", "011_security_price_observations.sql", "012_kiwoom_security_price_observations.sql", "013_instrument_listing_events.sql", "022_ledger_replay_policy.sql", "023_benchmark_series_versions.sql", "024_owner_fx_series_versions.sql", "025_owner_price_series_versions.sql"} {
		script, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for version := 1; version <= 27; version++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, version, "2026-01-10T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	now := func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
	registry := observation.New(db, now)
	serial := 0
	id := func(prefix string) string { serial++; return fmt.Sprintf("%s_%d", prefix, serial) }
	l, err := ledger.Compose(ledger.Config{DB: db, Observations: registry, Now: now, ID: id, PreviewTTL: func() time.Duration { return time.Minute }})
	if err != nil {
		t.Fatal(err)
	}
	m, err := market.Compose(market.Config{DB: db, Observations: registry, Ledger: l, Now: now, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return m, l, db, registry
}

func TestOwnerPriceAtomicPublicModulePreservesRetryAndProof(t *testing.T) {
	m, l, db, registry := marketFixture(t)
	ctx := context.Background()
	csv := []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,corrects_source_event_id\ncapital,account-main,DEPOSIT,2025-01-01T00:00:00Z,,,,,USD,100,\nbuy,account-main,BUY,2025-01-02T00:00:00Z,AAPL,1,100,0,USD,-100,\n")
	preview, issue := l.PreviewImport(ctx, csv)
	if issue != nil {
		t.Fatal(issue)
	}
	if _, issue := l.ApplyImport(ctx, lc.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: "seed"}); issue != nil {
		t.Fatal(issue)
	}
	api := market.ComposeOwnerPrice(&observations.OwnerPrice{Registry: registry, Ledger: l, Scoped: m.ScopedPrice}, l.CanonicalInstrumentID)
	first, err := api.ImportOwnerPrice(ctx, []byte(priceInput))
	if err != nil {
		t.Fatal(err)
	}
	second, err := api.ImportOwnerPrice(ctx, []byte(priceInput))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("retry changed receipt: %v %v", second, err)
	}
	if _, err := api.ImportOwnerPrice(ctx, []byte(strings.Replace(priceInput, `"200"`, `"201"`, 1))); !errors.Is(err, c.ErrConflict) {
		t.Fatalf("version conflict lost: %v", err)
	}
	if _, err := api.ImportOwnerPrice(ctx, []byte(strings.ReplaceAll(priceInput, "aapl", "msft"))); err == nil {
		t.Fatal("unregistered identity accepted")
	}
	err = registry.ReadValuation(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		inputs, err := m.Queries.ReadValuationInputs(ctx, ref, c.ValuationQuery{Prices: true})
		if err != nil {
			return err
		}
		if len(inputs.Prices) != 1 || inputs.Prices[0].Price != "200" || inputs.Prices[0].OwnerVersion != "price-v1" {
			t.Fatalf("price input changed: %+v", inputs)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM owner_price_series_versions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial/duplicate version count %d %v", count, err)
	}
}

func TestMarketProofRejectsForeignAndExpiredObservation(t *testing.T) {
	m, _, db, registry := marketFixture(t)
	var saved storage.ObservationRef
	if err := registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		saved = ref
		_, err := m.Queries.ReadValuationInputs(context.Background(), ref, c.ValuationQuery{})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Queries.ReadValuationInputs(context.Background(), saved, c.ValuationQuery{}); err == nil {
		t.Fatal("expired ref accepted")
	}
	other := observation.New(db, time.Now)
	if err := other.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		if _, err := m.Queries.ReadValuationInputs(context.Background(), ref, c.ValuationQuery{}); err == nil {
			t.Fatal("foreign registry ref accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBackupProofKeepsLegacyMarketAreasAbsent(t *testing.T) {
	m, _, db, registry := marketFixture(t)
	if _, err := db.Exec(`DROP TABLE owner_price_series_versions; DROP TABLE owner_fx_series_versions; DROP TABLE benchmark_series_versions; DELETE FROM schema_migrations WHERE version>17`); err != nil {
		t.Fatal(err)
	}
	err := registry.VerifyBackup(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		proof, err := m.Recovery.VerifyBackup(context.Background(), ref, c.BackupQuery{Schema: 17})
		if err != nil {
			return err
		}
		if proof.FXSHA256 == "" || proof.PricesSHA256 == "" || proof.ListingsSHA256 == "" || proof.OwnerPriceSHA256 != "" || proof.BenchmarkSHA256 != "" {
			t.Fatalf("legacy proof changed %+v", proof)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOwnerPriceDecodePrecedesCorruptLedgerProof(t *testing.T) {
	m, l, db, registry := marketFixture(t)
	if _, err := db.Exec(`UPDATE ledger_meta SET revision=7 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	api := market.ComposeOwnerPrice(&observations.OwnerPrice{Registry: registry, Ledger: l, Scoped: m.ScopedPrice}, l.CanonicalInstrumentID)
	if _, err := api.ImportOwnerPrice(context.Background(), []byte(`{"invalid":true}`)); !errors.Is(err, c.ErrOwnerPriceInput) {
		t.Fatalf("decode error lost precedence: %v", err)
	}
	if _, err := api.ImportOwnerPrice(context.Background(), []byte(priceInput)); err == nil {
		t.Fatal("corrupt ledger admitted price")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM owner_price_series_versions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed proof persisted price: %d %v", count, err)
	}
}
