package ledger

import (
	"database/sql"
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
	reader, err := config.Observations.LedgerReader(sqlite.ReadStatements())
	if err != nil {
		return nil, err
	}
	return sqlite.NewService(config.DB, config.Now, config.ID, config.PreviewTTL, config.CursorKey, reader), nil
}
