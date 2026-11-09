package application

import (
	"time"
)

type UseCases struct {
	journal                          UnitOfWork
	promotionReader                  PromotionReader
	now                              func() time.Time
	id                               func(string) string
	executionOwner, paperRunnerOwner string
}

func New(journal UnitOfWork, reader PromotionReader, clock func() time.Time, id func(string) string, executionOwner, runnerOwner string) *UseCases {
	return &UseCases{journal: journal, promotionReader: reader, now: clock, id: id, executionOwner: executionOwner, paperRunnerOwner: runnerOwner}
}
