package application

import (
	"time"
)

type UseCases struct {
	journal                          UnitOfWork
	now                              func() time.Time
	id                               func(string) string
	executionOwner, paperRunnerOwner string
}

func New(journal UnitOfWork, clock func() time.Time, id func(string) string, executionOwner, runnerOwner string) *UseCases {
	return &UseCases{journal: journal, now: clock, id: id, executionOwner: executionOwner, paperRunnerOwner: runnerOwner}
}
