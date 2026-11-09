package sqlite

import (
	"database/sql"
	"time"
)

type Store struct {
	db               *sql.DB
	now              func() time.Time
	id               func(string) string
	executionOwner   string
	paperRunnerOwner string
}

func New(db *sql.DB, clock func() time.Time, id func(string) string, executionOwner, runnerOwner string) *Store {
	return &Store{db: db, now: clock, id: id, executionOwner: executionOwner, paperRunnerOwner: runnerOwner}
}
