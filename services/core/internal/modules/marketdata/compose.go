package marketdata

import (
	"database/sql"
	"errors"
	"time"

	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
	identity "omni-folio/services/core/internal/modules/marketdata/internal/adapters/ledger"
	"omni-folio/services/core/internal/modules/marketdata/internal/adapters/sqlite"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"omni-folio/services/core/internal/storage/observation"
)

type Config struct {
	DB           *sql.DB
	Observations *observation.Registry
	Ledger       ledger.Queries
	Now          func() time.Time
	ID           func(string) string
}

type Module struct {
	Queries      c.Queries
	Imports      c.Imports
	Observations c.Observations
	ScopedPrice  c.ScopedOwnerPriceImport
	Recovery     c.Recovery
}

func Compose(config Config) (Module, error) {
	if !config.Observations.MatchesDB(config.DB) || config.Ledger == nil || config.Now == nil || config.ID == nil {
		return Module{}, errors.New("market composition is incomplete or references another database")
	}
	reader, err := config.Observations.MarketReader(sqlite.ReadStatements())
	if err != nil {
		return Module{}, err
	}
	writer, err := config.Observations.OwnerPriceWriter(sqlite.ReadStatements(), sqlite.OwnerPriceStatements())
	if err != nil {
		return Module{}, err
	}
	store := sqlite.New(config.DB, reader, writer, config.Ledger.CanonicalInstrumentID)
	decoder := codec.New(config.Ledger.CanonicalInstrumentID)
	service := application.New(store, decoder, identity.New(config.Ledger), config.Now, config.ID)
	scoped := application.NewScopedImporter(store, config.Now)
	return Module{Queries: service, Imports: service, Observations: service, ScopedPrice: scoped, Recovery: service}, nil
}

func ComposeOwnerPrice(atomic c.AtomicOwnerPriceImport, canonical func(string) string) c.OwnerPriceImports {
	return application.NewOwnerPriceImporter(atomic, codec.New(canonical))
}
