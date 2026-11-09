package trading

import (
	"database/sql"
	"errors"
	"time"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/adapters/sqlite"
)

type Config struct {
	DB             *sql.DB
	Clock          func() time.Time
	ID             func(string) string
	ExecutionOwner string
	RunnerOwner    string
}

func Compose(config Config) (contracts.Trading, error) {
	if config.DB == nil || config.Clock == nil || config.ID == nil || config.ExecutionOwner == "" || config.RunnerOwner == "" {
		return nil, errors.New("trading configuration is incomplete")
	}
	return sqlite.New(config.DB, config.Clock, config.ID, config.ExecutionOwner, config.RunnerOwner), nil
}
