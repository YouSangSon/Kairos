package marketdata

import (
	"context"
	"database/sql"
	"errors"
	"time"

	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
	"omni-folio/services/core/internal/modules/marketdata/internal/adapters/sqlite"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	storage "omni-folio/services/core/internal/storage/contracts"
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
	service := application.New(store, decoder, ledgerAdapter{config.Ledger}, config.Now, config.ID)
	scoped := application.NewScopedImporter(store, decoder, config.Now)
	return Module{Queries: service, Imports: service, Observations: service, ScopedPrice: scoped, Recovery: service}, nil
}

func ComposeOwnerPrice(atomic c.AtomicOwnerPriceImport) c.AtomicOwnerPriceImport {
	return application.NewOwnerPriceImporter(atomic)
}

type ledgerAdapter struct{ ledger.Queries }

func (a ledgerAdapter) AdmittedPriceIdentities(ctx context.Context, ref storage.ObservationRef) (c.PriceIdentities, error) {
	values, err := a.Queries.AdmittedPriceIdentities(ctx, ref)
	if err != nil {
		return c.PriceIdentities{}, err
	}
	result := c.PriceIdentities{LedgerRevision: values.LedgerRevision, Identities: make(map[string]struct{}, len(values.Identities))}
	for _, value := range values.Identities {
		result.Identities[value.InstrumentID+"\x00"+value.Currency] = struct{}{}
	}
	return result, nil
}
