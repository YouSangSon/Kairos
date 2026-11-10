package maintenance

import (
	"database/sql"
	"errors"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/adapters/sqlite"
	"time"
)

type VerificationConfig struct {
	Now       func() time.Time
	ID        func(string) string
	NewOwners func(*sql.DB) (Owners, error)
}

func ComposeVerification(config VerificationConfig) (c.ManifestVerifier, error) {
	if config.Now == nil || config.ID == nil || config.NewOwners == nil {
		return nil, errors.New("maintenance verification composition is incomplete")
	}
	factory := func(db *sql.DB) (sqlite.Owners, error) {
		owners, err := config.NewOwners(db)
		return sqlite.Owners{Observations: owners.Observations, Ledger: owners.Ledger, Market: owners.Market, Trading: owners.Trading, Watchlist: owners.Watchlist}, err
	}
	return sqlite.New(nil, nil, nil, config.Now, config.ID, factory), nil
}
