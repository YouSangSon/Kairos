package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omni-folio/services/core/internal/composition/observations"
	ledger "omni-folio/services/core/internal/modules/ledger"
	lc "omni-folio/services/core/internal/modules/ledger/contracts"
	mc "omni-folio/services/core/internal/modules/marketdata/contracts"
	marketcodec "omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
	identity "omni-folio/services/core/internal/modules/marketdata/internal/adapters/ledger"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	storagepool "omni-folio/services/core/internal/storage/sqlite"
)

type conservationFixture struct {
	*application.Service
	db         *sql.DB
	ledger     lc.Service
	registry   *observation.Registry
	ownerPrice mc.OwnerPriceImports
	now        func() time.Time
	id         func(string) string
}

func conservationService(t *testing.T, times []time.Time, ids map[string][]string) (*conservationFixture, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "market.db")
	db, err := storagepool.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"001_init.sql", "003_broker_snapshots.sql", "005_ledger_events.sql", "008_cash_void.sql", "009_fx_exchange.sql", "010_fx_observations.sql", "011_security_price_observations.sql", "012_kiwoom_security_price_observations.sql", "013_instrument_listing_events.sql", "022_ledger_replay_policy.sql", "023_benchmark_series_versions.sql", "024_owner_fx_series_versions.sql", "025_owner_price_series_versions.sql"} {
		script, err := migrationFiles.ReadFile("migrations/" + name)
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
	f := &conservationFixture{db: db, now: clock, id: id}
	// Session cutoffs stay independent of the import clock, including clock-free retries.
	f.registry = observation.New(db, func() time.Time { return priceMustTime("2026-01-10T15:01:00Z") })
	f.ledger, err = ledger.Compose(ledger.Config{DB: db, Observations: f.registry, Now: func() time.Time { return f.now() }, ID: func(prefix string) string { return f.id(prefix) }, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := f.registry.MarketReader(ReadStatements())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.registry.OwnerPriceWriter(ReadStatements(), OwnerPriceStatements())
	if err != nil {
		t.Fatal(err)
	}
	store := New(db, reader, writer, f.ledger.CanonicalInstrumentID)
	decoder := marketcodec.New(f.ledger.CanonicalInstrumentID)
	f.Service = application.New(store, decoder, identity.New(f.ledger), func() time.Time { return f.now() }, func(prefix string) string { return f.id(prefix) })
	f.ownerPrice = application.NewOwnerPriceImporter(&observations.OwnerPrice{Registry: f.registry, Ledger: f.ledger, Scoped: application.NewScopedImporter(store, func() time.Time { return f.now() })}, decoder)
	return f, path
}

func conservationApplication(db *sql.DB, l lc.Service, now func() time.Time, id func(string) string) *application.Service {
	return application.New(New(db, nil, nil, l.CanonicalInstrumentID), marketcodec.New(l.CanonicalInstrumentID), identity.New(l), now, id)
}
func (f *conservationFixture) reader() queryer {
	return databaseReader{f.db, f.ledger.CanonicalInstrumentID}
}
func (f *conservationFixture) admitted(t *testing.T) map[string]struct{} {
	t.Helper()
	var identities map[string]struct{}
	if err := f.registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		admitted, err := f.ledger.AdmittedPriceIdentities(context.Background(), ref)
		if err != nil {
			return err
		}
		identities = make(map[string]struct{}, len(admitted.Identities))
		for _, value := range admitted.Identities {
			identities[value.InstrumentID+"\x00"+value.Currency] = struct{}{}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return identities
}
func recordXIRRFX(t *testing.T, svc *conservationFixture, source, rate, observed, fetched, recorded string) {
	t.Helper()
	previous := svc.now
	svc.now = func() time.Time { return priceMustTime(recorded) }
	defer func() { svc.now = previous }()
	if _, err := svc.RecordFX(context.Background(), FXObservationInput{Source: "local_fixture", SourceObservationID: source, BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: rate, ObservedAt: observed, FetchedAt: fetched}); err != nil {
		t.Fatal(err)
	}
}
func applyTestCSV(t *testing.T, svc *conservationFixture, key, body string) {
	t.Helper()
	preview, appErr := svc.ledger.PreviewImport(context.Background(), []byte(body))
	if appErr != nil {
		t.Fatal(appErr)
	}
	if !preview.CanApply {
		t.Fatalf("test setup preview was rejected: %+v", preview)
	}
	if _, appErr := svc.ledger.ApplyImport(context.Background(), lc.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: key}); appErr != nil {
		t.Fatal(appErr)
	}
}
func testCSV(rows ...string) string {
	return "source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount\n" + strings.Join(rows, "\n") + "\n"
}
func conservationContracts() string { return "../../../../../../../../contracts" }
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(conservationContracts(), "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
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
func jsonKeySet(value map[string]any) map[string]bool {
	result := make(map[string]bool, len(value))
	for key := range value {
		result[key] = true
	}
	return result
}
func jsonStringSet(value any) map[string]bool {
	items := value.([]any)
	result := make(map[string]bool, len(items))
	for _, item := range items {
		result[item.(string)] = true
	}
	return result
}
