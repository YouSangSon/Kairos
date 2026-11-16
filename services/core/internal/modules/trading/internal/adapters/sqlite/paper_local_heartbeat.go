package sqlite

import (
	"context"
)

func (s *Store) heartbeatLocalPaperExecution(ctx context.Context, claim *paperRunnerClaim, fence int64) (*paperRunnerClaim, *ExecutionAuthorityState, error) {
	return s.business().HeartbeatLocalPaperExecution(ctx, claim, fence)
}
