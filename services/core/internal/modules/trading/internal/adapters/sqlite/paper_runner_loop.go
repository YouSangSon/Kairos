package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"time"
)

const (
	paperRunnerLoopHeartbeatInterval  = 10 * time.Second
	paperRunnerLoopDueEveryHeartbeats = 6
	paperRunnerLoopReleaseTimeout     = 5 * time.Second
)

func (s *Store) RunPaperPerformanceLoop(ctx context.Context, accountRef string) error {
	return s.business().RunPaperPerformanceLoop(ctx, accountRef)
}

func (s *Store) runPaperPerformanceLoopWithWait(ctx context.Context, accountRef string, wait func(context.Context, time.Duration) error) (resultErr error) {
	return s.business().RunPaperPerformanceLoopWithWait(ctx, accountRef, wait)
}

func (s *Store) runPaperPerformanceLoopDue(ctx context.Context, accountRef string, claim *paperRunnerClaim) (bool, error) {
	return s.business().RunPaperPerformanceLoopDue(ctx, accountRef, claim)
}

var waitPaperRunnerLoop = application.WaitPaperRunnerLoop
