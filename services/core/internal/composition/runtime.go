package composition

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"omni-folio/services/core/internal/composition/observations"
	ledger "omni-folio/services/core/internal/modules/ledger"
	lc "omni-folio/services/core/internal/modules/ledger/contracts"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	market "omni-folio/services/core/internal/modules/marketdata"
	mc "omni-folio/services/core/internal/modules/marketdata/contracts"
	portfolio "omni-folio/services/core/internal/modules/portfolio"
	pc "omni-folio/services/core/internal/modules/portfolio/contracts"
	research "omni-folio/services/core/internal/modules/research"
	rc "omni-folio/services/core/internal/modules/research/contracts"
	trading "omni-folio/services/core/internal/modules/trading"
	tc "omni-folio/services/core/internal/modules/trading/contracts"
	watchlist "omni-folio/services/core/internal/modules/watchlist"
	provider "omni-folio/services/core/internal/providers/kiwoom"
	"omni-folio/services/core/internal/storage/observation"
)

type Config struct {
	RequireStartup    bool
	PreviewTTL        func() time.Duration
	MarketFixturePath string
	ResearchFeedPath  string
	DB                *sql.DB
	Now               func() time.Time
	ID                func(string) string
	MockBrokerClient  *provider.Client
}

type Runtime struct {
	Ledger            lc.Service
	Market            market.Module
	Trading           tc.Trading
	Portfolio         pc.Queries
	Watchlist         watchlist.Module
	Research          rc.Queries
	Maintenance       maintenance.Module
	OwnerPrice        mc.OwnerPriceImports
	db                *sql.DB
	now               func() time.Time
	activityCursorKey [32]byte
}

func Compose(config Config) (*Runtime, error) {
	if config.DB == nil || config.Now == nil || config.ID == nil {
		return nil, errors.New("core composition requires a database, clock and ID generator")
	}
	r := &Runtime{db: config.DB, now: config.Now}
	if _, err := rand.Read(r.activityCursorKey[:]); err != nil {
		return nil, err
	}
	clock := config.Now
	id := config.ID
	previewTTL := config.PreviewTTL
	if previewTTL == nil {
		previewTTL = func() time.Duration { return 15 * time.Minute }
	}
	registry := observation.New(config.DB, clock)
	l, err := ledger.Compose(ledger.Config{DB: config.DB, Observations: registry, Now: clock, ID: id, PreviewTTL: previewTTL, CursorKey: r.activityCursorKey})
	if err != nil {
		return nil, err
	}
	t, err := trading.Compose(trading.Config{DB: config.DB, Observations: registry, Clock: clock, ID: id, ExecutionOwner: randomID("execution_owner"), RunnerOwner: randomID("paper_runner_owner"), MockBrokerClient: config.MockBrokerClient})
	if err != nil {
		return nil, err
	}
	r.Maintenance, err = maintenance.Compose(maintenance.Config{DB: config.DB, Observations: registry, Trading: t, Now: clock, ID: id, NewOwners: func(db *sql.DB) (maintenance.Owners, error) { return recoveryOwners(db, clock, id) }})
	if err != nil {
		return nil, err
	}
	if config.RequireStartup {
		if err := r.Maintenance.Schema.RequireStartup(context.Background()); err != nil {
			return nil, err
		}
	}
	m, err := market.Compose(market.Config{FixturePath: config.MarketFixturePath, DB: config.DB, Observations: registry, Ledger: l, Now: clock, ID: id})
	if err != nil {
		return nil, err
	}
	wInputs := &observations.Watchlist{Registry: registry, Market: m.Queries}
	w, err := watchlist.Compose(config.DB, registry, wInputs, clock)
	if err != nil {
		return nil, err
	}
	wInputs.States = w.States
	r.Ledger, r.Market, r.Trading, r.Watchlist = l, m, t, w
	r.Portfolio = portfolio.Compose(observations.Portfolio{Registry: registry, Ledger: l, Market: m.Queries}, clock)
	r.Research = research.Compose(research.Config{FeedPath: func() string { return config.ResearchFeedPath }, Now: clock})
	r.OwnerPrice = market.ComposeOwnerPrice(&observations.OwnerPrice{Registry: registry, Ledger: l, Scoped: m.ScopedPrice}, l.CanonicalInstrumentID)
	return r, nil
}

func recoveryOwners(db *sql.DB, clock func() time.Time, id func(string) string) (maintenance.Owners, error) {
	registry := observation.New(db, clock)
	l, err := ledger.Compose(ledger.Config{DB: db, Observations: registry, Now: clock, ID: id, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		return maintenance.Owners{}, err
	}
	m, err := market.Compose(market.Config{DB: db, Observations: registry, Ledger: l, Now: clock, ID: id})
	if err != nil {
		return maintenance.Owners{}, err
	}
	t, err := trading.Compose(trading.Config{DB: db, Observations: registry, Clock: clock, ID: id, ExecutionOwner: randomID("execution_owner"), RunnerOwner: randomID("paper_runner_owner")})
	if err != nil {
		return maintenance.Owners{}, err
	}
	wInputs := &observations.Watchlist{Registry: registry, Market: m.Queries}
	w, err := watchlist.Compose(db, registry, wInputs, clock)
	if err != nil {
		return maintenance.Owners{}, err
	}
	wInputs.States = w.States
	return maintenance.Owners{Observations: registry, Ledger: l, Market: m.Recovery, Trading: t, Watchlist: w.Recovery}, nil
}

func randomID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}

func (r *Runtime) Close() error { return r.db.Close() }
