package maintenance

import (
	"database/sql"
	"errors"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/adapters/sqlite"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	watch "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type Owners struct {
	Observations *observation.Registry
	Ledger       ledger.Maintenance
	Market       market.Recovery
	Trading      trading.Recovery
	Watchlist    watch.Recovery
}
type Config struct {
	DB           *sql.DB
	Observations *observation.Registry
	Trading      trading.Recovery
	Now          func() time.Time
	ID           func(string) string
	NewOwners    func(*sql.DB) (Owners, error)
}
type Module struct {
	Schema     c.Schema
	Migrations c.Migrations
	Backups    c.Backups
}

func Compose(config Config) (Module, error) {
	if !config.Observations.MatchesDB(config.DB) || config.Trading == nil || config.Now == nil || config.ID == nil || config.NewOwners == nil {
		return Module{}, errors.New("maintenance composition is incomplete or references another database")
	}
	factory := func(db *sql.DB) (sqlite.Owners, error) {
		owners, err := config.NewOwners(db)
		return sqlite.Owners{Observations: owners.Observations, Ledger: owners.Ledger, Market: owners.Market, Trading: owners.Trading, Watchlist: owners.Watchlist}, err
	}
	store := sqlite.New(config.DB, config.Observations, config.Trading, config.Now, config.ID, factory)
	return Module{Migrations: store, Backups: store, Schema: store}, nil
}
