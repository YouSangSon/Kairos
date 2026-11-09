package maintenance

import (
	"database/sql"
	"errors"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/adapters/sqlite"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/storage/observation"
)

type Config struct {
	DB           *sql.DB
	Observations *observation.Registry
	Trading      trading.Recovery
}
type Module struct{ Migrations c.Migrations }

func Compose(config Config) (Module, error) {
	if !config.Observations.MatchesDB(config.DB) || config.Trading == nil {
		return Module{}, errors.New("maintenance composition is incomplete or references another database")
	}
	store := sqlite.New(config.DB, config.Observations, config.Trading)
	return Module{Migrations: store}, nil
}
