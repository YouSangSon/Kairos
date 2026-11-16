package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

type localPaperRun struct {
	service            *Store
	account, selection string
	claim              *paperRunnerClaim
	lease              *ExecutionAuthorityState
}

func (r *localPaperRun) step(ctx context.Context, p, b, research []byte) (*LocalPaperStepResult, error) {
	run := &application.LocalPaperRun{Service: r.service.business(), Account: r.account, Selection: r.selection, Claim: r.claim, Lease: r.lease}
	result, err := run.Step(ctx, p, b, research)
	r.claim, r.lease = run.Claim, run.Lease
	return result, err
}
func (r *localPaperRun) Close() error {
	run := &application.LocalPaperRun{Service: r.service.business(), Account: r.account, Selection: r.selection, Claim: r.claim, Lease: r.lease}
	return run.Close()
}
