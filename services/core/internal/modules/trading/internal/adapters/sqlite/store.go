package sqlite

import (
	"database/sql"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type Store struct {
	backupReader, migrationReader *observation.ReadResolver
	dispatcher                    application.Dispatcher
	db                            *database
	now                           func() time.Time
	id                            func(string) string
	executionOwner                string
	paperRunnerOwner              string
}

func New(db *sql.DB, clock func() time.Time, id func(string) string, executionOwner, runnerOwner string) *Store {
	return &Store{db: &database{db}, now: clock, id: id, executionOwner: executionOwner, paperRunnerOwner: runnerOwner}
}

func (s *Store) BindDispatcher(dispatcher application.Dispatcher) { s.dispatcher = dispatcher }
