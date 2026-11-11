package application

import (
	"context"
	"time"
)

type UseCases struct {
	wait                             func(context.Context, time.Duration) error
	reads                            ReadUnitOfWork
	inputs                           InputDecoder
	journal                          UnitOfWork
	promotionReader                  PromotionReader
	now                              func() time.Time
	id                               func(string) string
	executionOwner, paperRunnerOwner string
}

func New(journal UnitOfWork, reader PromotionReader, clock func() time.Time, id func(string) string, executionOwner, runnerOwner string) *UseCases {
	return &UseCases{journal: journal, promotionReader: reader, now: clock, id: id, executionOwner: executionOwner, paperRunnerOwner: runnerOwner}
}

func (s *UseCases) WithInputs(inputs InputDecoder) *UseCases { s.inputs = inputs; return s }

func (s *UseCases) WithReads(reads ReadUnitOfWork) *UseCases { s.reads = reads; return s }

func (s *UseCases) WithWait(wait func(context.Context, time.Duration) error) *UseCases {
	s.wait = wait
	return s
}
