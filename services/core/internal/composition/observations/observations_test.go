package observations

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	portfolio "omni-folio/services/core/internal/modules/portfolio/contracts"
	watchmodule "omni-folio/services/core/internal/modules/watchlist"
	watch "omni-folio/services/core/internal/modules/watchlist/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
)

// The owner probes read SQLite through the same allowlisted resolvers used by owner adapters.
type ledgerProbe struct {
	reader    *observation.ReadResolver
	afterRead func()
	seen      storage.ObservationRef
}

func (l *ledgerProbe) ValuationFacts(ctx context.Context, ref storage.ObservationRef) (ledger.ValuationFacts, error) {
	h, err := l.reader.Resolve(ref)
	if err != nil {
		return ledger.ValuationFacts{}, err
	}
	var revision int64
	if err := h.QueryOne(ctx, `SELECT revision FROM ledger_meta`).Scan(&revision); err != nil {
		return ledger.ValuationFacts{}, err
	}
	l.seen = ref
	if l.afterRead != nil {
		l.afterRead()
	}
	return ledger.ValuationFacts{Revision: revision}, nil
}

type marketProbe struct {
	reader         *observation.ReadResolver
	ledger         *ledgerProbe
	seen           storage.ObservationRef
	ledgerRevision int64
	afterRead      func()
	wrongCutoff    bool
}

func (m *marketProbe) ReadValuationInputs(ctx context.Context, ref storage.ObservationRef, query market.ValuationQuery) (market.ValuationInputs, error) {
	if m.ledger != nil {
		proof, err := m.ledger.ValuationFacts(ctx, ref)
		if err != nil {
			return market.ValuationInputs{}, err
		}
		m.ledgerRevision = proof.Revision
	}
	h, err := m.reader.Resolve(ref)
	if err != nil {
		return market.ValuationInputs{}, err
	}
	cutoff, err := m.reader.Cutoff(ref)
	if err != nil {
		return market.ValuationInputs{}, err
	}
	var price string
	if err := h.QueryOne(ctx, `SELECT price FROM security_price_observations`).Scan(&price); err != nil {
		return market.ValuationInputs{}, err
	}
	m.seen = ref
	if m.afterRead != nil {
		m.afterRead()
	}
	if m.wrongCutoff {
		cutoff = cutoff.Add(time.Nanosecond)
	}
	return market.ValuationInputs{Cutoff: cutoff, Prices: []market.SecurityPriceObservation{{Price: price}}}, nil
}
func fixture(t *testing.T) (*sql.DB, *sql.DB, *observation.Registry, *ledgerProbe, *marketProbe, time.Time) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "observations.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	writer.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec(`CREATE TABLE ledger_meta(revision INTEGER);INSERT INTO ledger_meta VALUES(1);CREATE TABLE security_price_observations(price TEXT);INSERT INTO security_price_observations VALUES('100')`); err != nil {
		t.Fatal(err)
	}
	script, err := migrations.Files.ReadFile("026_watchlist_state.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	registry := observation.New(db, func() time.Time { return at })
	lr, err := registry.LedgerReader([]observation.Statement{{SQL: `SELECT revision FROM ledger_meta`}})
	if err != nil {
		t.Fatal(err)
	}
	mr, err := registry.MarketReader([]observation.Statement{{SQL: `SELECT price FROM security_price_observations`}})
	if err != nil {
		t.Fatal(err)
	}
	l := &ledgerProbe{reader: lr}
	m := &marketProbe{reader: mr}
	return db, writer, registry, l, m, at
}
func writeBetweenReads(t *testing.T, writer *sql.DB) func() {
	t.Helper()
	return func() {
		done := make(chan error, 1)
		go func() {
			_, err := writer.Exec(`UPDATE ledger_meta SET revision=2;UPDATE security_price_observations SET price='200'`)
			done <- err
		}()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
func TestMOD11PortfolioCoordinatorKeepsOneSQLiteSnapshot(t *testing.T) {
	_, writer, registry, l, m, at := fixture(t)
	l.afterRead = writeBetweenReads(t, writer)
	c := Portfolio{Registry: registry, Ledger: l, Market: m}
	all, err := c.ReadValuationInputs(context.Background(), portfolio.InputQuery{Prices: true})
	if err != nil || all.Ledger.Revision != 1 || all.Market.Prices[0].Price != "100" || !all.Cutoff.Equal(at) || l.seen != m.seen {
		t.Fatalf("mixed observation: %+v %v", all, err)
	}
	if _, err := l.reader.Resolve(l.seen); !errors.Is(err, observation.ErrReference) {
		t.Fatal("closed ref remained available", err)
	}
	l.afterRead = nil
	next, err := c.ReadValuationInputs(context.Background(), portfolio.InputQuery{Prices: true})
	if err != nil || next.Ledger.Revision != 2 || next.Market.Prices[0].Price != "200" {
		t.Fatalf("next snapshot missed writer: %+v %v", next, err)
	}
	requested := at.Add(-time.Hour)
	past, err := c.ReadValuationInputs(context.Background(), portfolio.InputQuery{At: &requested, Prices: true})
	if err != nil || !past.Cutoff.Equal(requested) || !past.Market.Cutoff.Equal(requested) {
		t.Fatal("requested cutoff escaped observation", err)
	}
	m.wrongCutoff = true
	if _, err := c.ReadValuationInputs(context.Background(), portfolio.InputQuery{Prices: true}); err == nil {
		t.Fatal("mixed market cutoff accepted")
	}
	foreign := observation.New(writer, func() time.Time { return at })
	if err := foreign.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		_, err := m.ReadValuationInputs(context.Background(), ref, market.ValuationQuery{Prices: true})
		if !errors.Is(err, observation.ErrReference) {
			t.Fatal("foreign reference accepted", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadValuationInputs(context.Background(), storage.ObservationRef{}, market.ValuationQuery{}); !errors.Is(err, observation.ErrReference) {
		t.Fatal("zero reference accepted", err)
	}
}

type stateBarrier struct {
	reader watch.StateReader
	after  func()
}

func (s stateBarrier) ReadState(ctx context.Context, ref storage.ObservationRef) (watch.WatchlistState, error) {
	state, err := s.reader.ReadState(ctx, ref)
	if err == nil && s.after != nil {
		s.after()
	}
	return state, err
}
func TestMOD16WatchlistCoordinatorKeepsStateQuoteAndProofTogether(t *testing.T) {
	db, writer, registry, l, m, at := fixture(t)
	m.ledger = l
	input := &Watchlist{Registry: registry, Market: m}
	mod, err := watchmodule.Compose(db, registry, input, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	input.States = mod.States
	writerRegistry := observation.New(writer, func() time.Time { return at })
	writerMod, err := watchmodule.Compose(writer, writerRegistry, nil, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	barrier := make(chan struct{})
	done := make(chan error, 1)
	input.States = stateBarrier{reader: mod.States, after: func() {
		close(barrier)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}}
	go func() {
		<-barrier
		_, err := writerMod.API.Replace(context.Background(), watch.WatchlistChange{ExpectedRevision: 0, Entries: []watch.WatchlistEntry{{Venue: "XNAS", Symbol: "AAPL", Currency: "USD"}}})
		if err == nil {
			_, err = writer.Exec(`UPDATE ledger_meta SET revision=2;UPDATE security_price_observations SET price='200'`)
		}
		done <- err
	}()
	all, err := input.ReadWatchlistInputs(context.Background())
	if err != nil || all.State.Revision != 0 || all.Prices[0].Price != "100" || m.ledgerRevision != 1 || l.seen != m.seen {
		t.Fatalf("watchlist mixed snapshot: %+v %v", all, err)
	}
	input.States = mod.States
	next, err := input.ReadWatchlistInputs(context.Background())
	if err != nil || next.State.Revision != 1 || next.Prices[0].Price != "200" || m.ledgerRevision != 2 {
		t.Fatalf("next watchlist snapshot: %+v %v", next, err)
	}
	if _, err := mod.States.ReadState(context.Background(), m.seen); !errors.Is(err, observation.ErrReference) {
		t.Fatal("watchlist ref escaped", err)
	}
	m.wrongCutoff = true
	if _, err := input.ReadWatchlistInputs(context.Background()); err == nil {
		t.Fatal("wrong watchlist cutoff accepted")
	}
}
