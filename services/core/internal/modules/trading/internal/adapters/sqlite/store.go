package sqlite

import (
	"database/sql"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"time"
)

type Store struct {
	dispatcher       application.Dispatcher
	db               *sql.DB
	now              func() time.Time
	id               func(string) string
	executionOwner   string
	paperRunnerOwner string
}

func New(db *sql.DB, clock func() time.Time, id func(string) string, executionOwner, runnerOwner string) *Store {
	return &Store{db: db, now: clock, id: id, executionOwner: executionOwner, paperRunnerOwner: runnerOwner}
}

func (s *Store) BindDispatcher(dispatcher application.Dispatcher) { s.dispatcher = dispatcher }
