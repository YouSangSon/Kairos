package observations_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"omni-folio/services/core/internal/composition/observations"
	ledger "omni-folio/services/core/internal/modules/ledger"
	lc "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata"
	mc "omni-folio/services/core/internal/modules/marketdata/contracts"
	portfolio "omni-folio/services/core/internal/modules/portfolio"
	pc "omni-folio/services/core/internal/modules/portfolio/contracts"
	watch "omni-folio/services/core/internal/modules/watchlist"
	wc "omni-folio/services/core/internal/modules/watchlist/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
)

type moduleFixture struct {
	reader, writer *sql.DB
	registry       *observation.Registry
	ledger, next   lc.Service
	market, update market.Module
	clock          func() time.Time
	writerRegistry *observation.Registry
	reads          *barrierLedger
}

func newModuleFixture(t *testing.T) moduleFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "modules.db") + "?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000"
	open := func() *sql.DB {
		db, err := sql.Open("sqlite3", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		return db
	}
	f := moduleFixture{reader: open(), writer: open(), clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }}
	// These are real owner schemas; unrelated trading journals are not needed by this composition.
	for _, name := range []string{"001_init.sql", "003_broker_snapshots.sql", "005_ledger_events.sql", "008_cash_void.sql", "009_fx_exchange.sql", "010_fx_observations.sql", "011_security_price_observations.sql", "012_kiwoom_security_price_observations.sql", "013_instrument_listing_events.sql", "022_ledger_replay_policy.sql", "023_benchmark_series_versions.sql", "024_owner_fx_series_versions.sql", "025_owner_price_series_versions.sql", "026_watchlist_state.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.reader.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for version := 1; version <= 27; version++ {
		if _, err := f.reader.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, version, "2026-01-10T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	var serial atomic.Uint64
	id := func(prefix string) string { return fmt.Sprintf("%s_%d", prefix, serial.Add(1)) }
	compose := func(db *sql.DB) (*observation.Registry, lc.Service, market.Module) {
		r := observation.New(db, f.clock)
		l, err := ledger.Compose(ledger.Config{DB: db, Observations: r, Now: f.clock, ID: id, PreviewTTL: func() time.Duration { return time.Minute }})
		if err != nil {
			t.Fatal(err)
		}
		observed := &barrierLedger{Service: l}
		m, err := market.Compose(market.Config{DB: db, Observations: r, Ledger: observed, Now: f.clock, ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return r, observed, m
	}
	f.registry, f.ledger, f.market = compose(f.reader)
	f.reads = f.ledger.(*barrierLedger)
	f.writerRegistry, f.next, f.update = compose(f.writer)
	if err := applyModuleCSV(context.Background(), f.ledger, "seed", "capital,account-main,DEPOSIT,2025-01-01T00:00:00Z,,,,,USD,100,\nbuy,account-main,BUY,2025-01-02T00:00:00Z,AAPL,1,100,0,USD,-100,\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.record(context.Background(), f.registry, f.ledger, f.market, "old", "1370", "200", "2026-01-09T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return f
}

func applyModuleCSV(ctx context.Context, l lc.Service, key, rows string) error {
	preview, issue := l.PreviewImport(ctx, []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,corrects_source_event_id\n"+rows))
	if issue != nil {
		return issue
	}
	_, issue = l.ApplyImport(ctx, lc.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: key})
	if issue != nil {
		return issue
	}
	return nil
}

func (f moduleFixture) record(ctx context.Context, r *observation.Registry, l lc.Service, m market.Module, version, rate, price, at string) error {
	if _, err := m.Observations.RecordFX(ctx, mc.FXObservationInput{Source: "local_fixture", SourceObservationID: version, BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: rate, ObservedAt: at, FetchedAt: at}); err != nil {
		return err
	}
	api := market.ComposeOwnerPrice(&observations.OwnerPrice{Registry: r, Ledger: l, Scoped: m.ScopedPrice}, l.CanonicalInstrumentID)
	raw := fmt.Sprintf(`{"schema_version":"owner-price-series.v1","source_version":"%s","instrument_id":"instrument_aapl","symbol":"AAPL","venue":"XNAS","currency":"USD","price_adjustment":"unadjusted","sample":false,"fetched_at":"%s","points":[{"observed_at":"%s","price":"%s"}]}`, version, at, at, price)
	_, err := api.ImportOwnerPrice(ctx, []byte(raw))
	return err
}

func (f moduleFixture) advance(ctx context.Context) error {
	if err := applyModuleCSV(ctx, f.next, "advance", "new-capital,account-main,DEPOSIT,2025-01-03T00:00:00Z,,,,,KRW,100,\n"); err != nil {
		return err
	}
	return f.record(ctx, f.writerRegistry, f.next, f.update, "new", "1400", "300", "2026-01-09T13:00:00Z")
}

type barrierLedger struct {
	lc.Service
	after    func() error
	seen     storage.ObservationRef
	admitted storage.ObservationRef
	revision string
}

func (l *barrierLedger) AdmittedPriceIdentities(ctx context.Context, ref storage.ObservationRef) (lc.AdmittedPrices, error) {
	result, err := l.Service.AdmittedPriceIdentities(ctx, ref)
	if err == nil {
		l.admitted, l.revision = ref, result.LedgerRevision
	}
	return result, err
}

func (l *barrierLedger) ValuationFacts(ctx context.Context, ref storage.ObservationRef) (lc.ValuationFacts, error) {
	facts, err := l.Service.ValuationFacts(ctx, ref)
	if err != nil {
		return lc.ValuationFacts{}, err
	}
	l.seen = ref
	if l.after != nil {
		if err := l.after(); err != nil {
			return lc.ValuationFacts{}, err
		}
	}
	return facts, nil
}

func moduleBarrier(t *testing.T, ctx context.Context, change func() error) func() error {
	t.Helper()
	start, done := make(chan struct{}), make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-start:
			done <- change()
		case <-ctx.Done():
			done <- ctx.Err()
		}
	}()
	t.Cleanup(func() {
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("observation writer did not exit")
		}
	})
	return func() error {
		close(start)
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func TestMOD11RealOwnerModulesPreservePortfolioObservation(t *testing.T) {
	f := newModuleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	l := f.reads
	l.after = moduleBarrier(t, ctx, func() error { return f.advance(ctx) })
	inputs := observations.Portfolio{Registry: f.registry, Ledger: l, Market: f.market.Queries}
	api := portfolio.Compose(inputs, f.clock)
	first, err := api.CurrentValuation(ctx)
	if err != nil || first == nil || first.LedgerRevision != "rev_0000000002" || first.TotalValue == nil || first.TotalValue.Amount != "274000" {
		t.Fatalf("mixed real owner snapshot: %+v %v", first, err)
	}
	if l.seen != l.admitted || l.revision != "rev_0000000002" {
		t.Fatalf("market admission escaped valuation snapshot: revision=%s", l.revision)
	}
	l.after = nil
	second, err := api.CurrentValuation(ctx)
	if err != nil || second == nil || second.LedgerRevision != "rev_0000000003" || second.TotalValue == nil || second.TotalValue.Amount != "420100" {
		t.Fatalf("next real owner snapshot missed commit: %+v %v", second, err)
	}
	if _, err := f.ledger.ValuationFacts(ctx, l.seen); err == nil {
		t.Fatal("real ledger accepted an expired observation")
	}
}

func TestValuationEvidencePreservesRealOwnerObservation(t *testing.T) {
	f := newModuleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	advance := moduleBarrier(t, ctx, func() error { return f.advance(ctx) })
	f.reads.after = func() error {
		f.reads.after = nil
		return advance()
	}
	api := portfolio.Compose(observations.Portfolio{Registry: f.registry, Ledger: f.reads, Market: f.market.Queries}, f.clock)
	cutoff := f.clock().Format(time.RFC3339Nano)

	type snapshot struct {
		revision, priceVersion, observedAt, price, rate string
		cash                                            []lc.Money
		cashKRW, holdingsKRW, totalKRW                  string
	}
	check := func(report *pc.PortfolioValuationEvidence, want snapshot) {
		t.Helper()
		if report.ObservationCutoff != cutoff {
			t.Fatalf("observation cutoff = %q, want %q", report.ObservationCutoff, cutoff)
		}
		for _, view := range []struct{ name, asOf, revision string }{
			{"cash", report.Cash.ValuationAsOf, report.Cash.LedgerRevision},
			{"holdings", report.Holdings.ValuationAsOf, report.Holdings.LedgerRevision},
			{"current", report.Current.ValuationAsOf, report.Current.LedgerRevision},
		} {
			if view.asOf != cutoff || view.revision != want.revision {
				t.Fatalf("%s valuation = %q at %q, want %q at %q", view.name, view.revision, view.asOf, want.revision, cutoff)
			}
		}
		if len(report.Cash.Lines) != len(want.cash) {
			t.Fatalf("%s cash lines = %+v, want native balances %+v", want.revision, report.Cash.Lines, want.cash)
		}
		for i, line := range report.Cash.Lines {
			balance := want.cash[i]
			if line.Currency != balance.Currency || line.Amount != balance.Amount || line.FX != nil {
				t.Fatalf("%s cash line = %+v, want unconverted %+v", want.revision, line, balance)
			}
		}
		if len(report.Holdings.Lines) != 1 {
			t.Fatalf("%s holdings = %+v, want one USD holding", want.revision, report.Holdings.Lines)
		}
		holding := report.Holdings.Lines[0]
		if holding.Symbol != "AAPL" || holding.Currency != "USD" || holding.Quantity != "1" || holding.CostBasis != "100" ||
			holding.MarketValue == nil || *holding.MarketValue != want.price {
			t.Fatalf("%s holding = %+v, want 1 AAPL at USD %s with USD 100 cost", want.revision, holding, want.price)
		}
		price := holding.Price
		if price == nil || price.SourceVersion != want.priceVersion || price.Price != want.price || price.ObservedAt != want.observedAt {
			t.Fatalf("%s holding price = %+v, want %s price %s observed %s", want.revision, price, want.priceVersion, want.price, want.observedAt)
		}
		if len(report.ConversionFX) != 1 {
			t.Fatalf("%s conversion FX = %+v, want one receipt for the USD holding", want.revision, report.ConversionFX)
		}
		fx := report.ConversionFX[0]
		if fx.Source != "local_fixture" || fx.ObservationID == "" || fx.BaseCurrency != "USD" || fx.QuoteCurrency != "KRW" ||
			fx.Rate != want.rate || fx.ObservedAt != want.observedAt || fx.FetchedAt != want.observedAt || fx.RecordedAt != cutoff {
			t.Fatalf("%s conversion FX = %+v, want stored USD/KRW rate %s observed %s", want.revision, fx, want.rate, want.observedAt)
		}
		for _, total := range []struct {
			name string
			got  *lc.Money
			want string
		}{
			{"cash total", report.Cash.Total, want.cashKRW},
			{"current cash", report.Current.CashValue, want.cashKRW},
			{"current holdings", report.Current.HoldingsValue, want.holdingsKRW},
			{"current total", report.Current.TotalValue, want.totalKRW},
		} {
			if total.got == nil || total.got.Currency != "KRW" || total.got.Amount != total.want {
				t.Fatalf("%s %s = %+v, want KRW %s", want.revision, total.name, total.got, total.want)
			}
		}
	}
	encode := func(report *pc.PortfolioValuationEvidence) string {
		t.Helper()
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	first, err := api.ValuationEvidence(ctx)
	if err != nil || first == nil {
		t.Fatalf("first report = %+v, err = %v", first, err)
	}
	check(first, snapshot{
		revision: "rev_0000000002", priceVersion: "old", observedAt: "2026-01-09T12:00:00Z", price: "200", rate: "1370",
		cash: []lc.Money{{Currency: "USD", Amount: "0"}}, cashKRW: "0", holdingsKRW: "274000", totalKRW: "274000",
	})
	before := encode(first)
	firstFXID := first.ConversionFX[0].ObservationID

	next, err := api.ValuationEvidence(ctx)
	if err != nil || next == nil {
		t.Fatalf("next report = %+v, err = %v", next, err)
	}
	check(next, snapshot{
		revision: "rev_0000000003", priceVersion: "new", observedAt: "2026-01-09T13:00:00Z", price: "300", rate: "1400",
		cash: []lc.Money{{Currency: "KRW", Amount: "100"}, {Currency: "USD", Amount: "0"}}, cashKRW: "100", holdingsKRW: "420000", totalKRW: "420100",
	})
	if next.ConversionFX[0].ObservationID == firstFXID {
		t.Fatal("new rate reused the previous FX observation identity")
	}
	if encode(first) != before {
		t.Fatal("first report changed after the next observation")
	}
}

type moduleStateBarrier struct {
	wc.StateReader
	after func() error
	seen  storage.ObservationRef
}

func (s *moduleStateBarrier) ReadState(ctx context.Context, ref storage.ObservationRef) (wc.WatchlistState, error) {
	state, err := s.StateReader.ReadState(ctx, ref)
	s.seen = ref
	if err == nil {
		err = s.after()
	}
	return state, err
}

func TestMOD16RealOwnerModulesPreserveWatchlistObservation(t *testing.T) {
	f := newModuleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inputs := &observations.Watchlist{Registry: f.registry, Market: f.market.Queries}
	mod, err := watch.Compose(f.reader, f.registry, inputs, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := watch.Compose(f.writer, f.writerRegistry, nil, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	entry := wc.WatchlistEntry{Venue: "XNAS", Symbol: "AAPL", Currency: "USD"}
	if _, err := writer.API.Replace(ctx, wc.WatchlistChange{ExpectedRevision: 0, Entries: []wc.WatchlistEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	state := &moduleStateBarrier{StateReader: mod.States, after: moduleBarrier(t, ctx, func() error {
		if _, err := writer.API.Replace(ctx, wc.WatchlistChange{ExpectedRevision: 1, Entries: []wc.WatchlistEntry{entry, {Venue: "XNAS", Symbol: "MSFT", Currency: "USD"}}}); err != nil {
			return err
		}
		return f.advance(ctx)
	})}
	inputs.States = state
	first, err := mod.API.Read(ctx)
	if err != nil || first == nil || first.Watchlist.Revision != 1 || len(first.Quotes) != 1 || first.Quotes[0].Price == nil || first.Quotes[0].Price.Price != "200" {
		t.Fatalf("mixed real watchlist snapshot: %+v %v", first, err)
	}
	if state.seen != f.reads.admitted || f.reads.revision != "rev_0000000002" {
		t.Fatalf("market admission escaped watchlist snapshot: revision=%s", f.reads.revision)
	}
	inputs.States = mod.States
	second, err := mod.API.Read(ctx)
	if err != nil || second == nil || second.Watchlist.Revision != 2 || len(second.Quotes) != 2 || second.Quotes[0].Price == nil || second.Quotes[0].Price.Price != "300" || second.Quotes[1].Price != nil {
		t.Fatalf("next real watchlist snapshot missed commit: %+v %v", second, err)
	}
}
