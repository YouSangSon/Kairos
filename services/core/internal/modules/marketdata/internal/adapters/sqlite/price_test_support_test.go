package sqlite

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	ledger "omni-folio/services/core/internal/modules/ledger"
	lc "omni-folio/services/core/internal/modules/ledger/contracts"
	marketcodec "omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
	identity "omni-folio/services/core/internal/modules/marketdata/internal/adapters/ledger"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
	"path/filepath"
	"testing"
	"time"
)

type priceTestFixture struct {
	*application.Service
	db     *sql.DB
	ledger lc.Service
}

func priceMustTime(raw string) time.Time {
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		panic(err)
	}
	return v
}
func (f *priceTestFixture) reader() queryer {
	return databaseReader{f.db, f.ledger.CanonicalInstrumentID}
}
func priceTestService(t *testing.T, times []time.Time, ids map[string][]string) (*priceTestFixture, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prices.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
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
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, version, "2026-01-10T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	clockIndex := 0
	clock := func() time.Time {
		if len(times) == 0 {
			return priceMustTime("2026-01-10T15:01:00Z").Add(time.Duration(clockIndex) * time.Millisecond)
		}
		if clockIndex >= len(times) {
			return times[len(times)-1].Add(time.Duration(clockIndex-len(times)+1) * time.Millisecond)
		}
		v := times[clockIndex]
		clockIndex++
		return v
	}
	counters := map[string]int{}
	id := func(prefix string) string {
		i := counters[prefix]
		counters[prefix]++
		if i < len(ids[prefix]) {
			return ids[prefix][i]
		}
		return prefix + "_test_" + time.Unix(0, int64(i)).Format("150405.000000000")
	}
	registry := observation.New(db, clock)
	l, err := ledger.Compose(ledger.Config{DB: db, Observations: registry, Now: clock, ID: id, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		t.Fatal(err)
	}
	store := New(db, nil, nil, l.CanonicalInstrumentID)
	service := application.New(store, marketcodec.New(l.CanonicalInstrumentID), identity.New(l), clock, id)
	return &priceTestFixture{service, db, l}, path
}
