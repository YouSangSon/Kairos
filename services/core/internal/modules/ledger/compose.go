package ledger

import (
	"database/sql"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/internal/modules/ledger/internal/adapters/sqlite"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type Config struct {
	DB         *sql.DB
	Now        func() time.Time
	ID         func(string) string
	PreviewTTL func() time.Duration
	CursorKey  [32]byte
	Reader     *observation.ReadResolver
}

func Compose(config Config) contracts.Service {
	return sqlite.NewService(config.DB, config.Now, config.ID, config.PreviewTTL, config.CursorKey, config.Reader)
}
func ReadStatements() []observation.Statement { return sqlite.ReadStatements() }
