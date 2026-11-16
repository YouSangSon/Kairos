package ledger

import (
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/internal/modules/ledger/internal/adapters/sqlite"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type Config struct {
	DB           *sql.DB
	Now          func() time.Time
	ID           func(string) string
	PreviewTTL   func() time.Duration
	CursorKey    [32]byte
	Observations *observation.Registry
}

func Compose(config Config) (contracts.Service, error) {
	if !config.Observations.MatchesDB(config.DB) {
		return nil, errors.New("ledger observation registry does not match database")
	}
	reader, err := config.Observations.LedgerReader(sqlite.ReadStatements())
	if err != nil {
		return nil, err
	}
	return sqlite.NewService(config.DB, config.Now, config.ID, config.PreviewTTL, config.CursorKey, reader), nil
}
