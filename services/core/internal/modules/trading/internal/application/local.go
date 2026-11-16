package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"time"
)

const capitalizedPaperSignalSchema = "paper-signal.v3"

type LocalPaperRun struct {
	Service            *UseCases
	Account, Selection string
	Claim              *RunnerClaim
	Lease              *ExecutionAuthorityState
}

func (s *UseCases) ExecuteLocalPaper(ctx context.Context, account, selection string, proposalRaw, barsRaw, researchRaw []byte) (result *LocalPaperStepResult, resultErr error) {
	run := &LocalPaperRun{Service: s, Account: account, Selection: selection}
	defer func() { resultErr = errors.Join(resultErr, ctx.Err(), run.Close()) }()
	return run.Step(ctx, proposalRaw, barsRaw, researchRaw)
}
func (r *LocalPaperRun) Close() error {
	var err error
	if r.Lease != nil {
		ctx, cancel := context.WithTimeout(context.Background(), paperRunnerLoopReleaseTimeout)
		err = r.Service.HaltOwnedSyntheticExecutionLease(ctx, r.Account, r.Lease.FencingToken)
		cancel()
	}
	if r.Claim != nil {
		ctx, cancel := context.WithTimeout(context.Background(), paperRunnerLoopReleaseTimeout)
		err = errors.Join(err, r.Service.ReleasePaperRunnerLease(ctx, r.Claim))
		cancel()
	}
	return err
}
func (r *LocalPaperRun) Heartbeat(ctx context.Context) error {
	if r.Lease == nil {
		return nil
	}
	// Policy stages can refresh the global claim independently. Schedule
	// against the execution expiry, never that mutable global heartbeat.
	expires, ok := canonicalUTCTime(r.Lease.LeaseExpiresAt)
	if !ok {
		return errors.New("local paper execution expiry is invalid")
	}
	if r.Service.now().Before(expires.Add(-syntheticExecutionLeaseTTL + paperRunnerLoopHeartbeatInterval)) {
		return nil
	}
	claim, lease, err := r.Service.HeartbeatLocalPaperExecution(ctx, r.Claim, r.Lease.FencingToken)
	if err == nil {
		r.Claim, r.Lease = claim, lease
	}
	return err
}
func (r *LocalPaperRun) Step(ctx context.Context, proposalRaw, barsRaw, researchRaw []byte) (*LocalPaperStepResult, error) {
	s, account, selection := r.Service, r.Account, r.Selection
	proposal, err := s.inputs.Proposal(proposalRaw)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(barsRaw)
	if hex.EncodeToString(hash[:]) != StringField(proposal, "input_sha256") {
		return nil, errors.New("paper proposal CSV hash differs")
	}
	if r.Claim == nil {
		claim, err := s.AcquirePaperRunnerLease(ctx, account)
		if err != nil {
			return nil, err
		}
		r.Claim = claim
	}
	if r.Lease != nil {
		if err := r.Heartbeat(ctx); err != nil {
			return nil, err
		}
		if err := s.checkLocalLease(ctx, account, r.Lease.FencingToken); err != nil {
			return nil, err
		}
	}
	if err := domain.ValidateLocalProposalSelection(selection, StringField(proposal, "strategy_result_sha256"), r.Claim.StrategySelectionEventID, r.Claim.SelectedResultSHA256); err != nil {
		return nil, err
	}
	facts, err := inspectResult(ctx, s.journal, func(tx InspectionScope) (LocalResearchFacts, error) {
		return tx.Selections().LocalResearch(ctx, r.Claim.SelectedResultSHA256)
	})
	if err != nil {
		return nil, err
	}
	slow, researchSHA := facts.Slow, facts.SHA256
	if err := s.inputs.Research(researchRaw, researchSHA, StringField(proposal, "symbol"), StringField(proposal, "data_as_of")); err != nil {
		return nil, err
	}
	snapshot, err := s.ImportPaperSnapshot(ctx, barsRaw)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateLocalWarmup(slow, snapshot.Bars); err != nil {
		return nil, err
	}
	committed, _, err := s.ProcessPaperProposal(ctx, account, selection, proposalRaw, snapshot.SignalBarObservationID, 0, false, r.Claim)
	if err != nil {
		return nil, err
	}
	// Preparation can age the global claim before execution acquires its own
	// lease. Align their lifetimes, retaining the old tuple if renewal fails.
	if r.Lease == nil {
		preparedClaim, err := s.HeartbeatPaperRunnerLease(ctx, r.Claim)
		if err != nil {
			return nil, err
		}
		r.Claim = preparedClaim
		lease, err := s.StartLocalPaperExecutionWithClaim(ctx, account, selection, r.Claim)
		if err != nil {
			return nil, err
		}
		r.Lease = lease
	}
	ids, err := s.OpenLocalPaperOrderIDs(ctx, account)
	if err != nil {
		return nil, err
	}
	closes, err := s.LocalPaperPolicyCloses(ctx, account, StringField(proposal, "data_as_of"), committed != nil)
	if err != nil {
		return nil, err
	}
	result := &LocalPaperStepResult{Mode: "paper_fixture_only", Snapshot: snapshot}
	for _, asOf := range closes {
		if err := r.Heartbeat(ctx); err != nil {
			return nil, err
		}
		// A committed valuation is a durable phase boundary. On restart,
		// complete its policy without adding fills behind that fixed cutoff.
		_, valued, err := s.loadPerformancePoint(ctx, account, asOf)
		if err != nil {
			return nil, err
		}
		if !valued {
			for _, id := range ids {
				state, err := s.loadLocalOrderState(ctx, id)
				if err != nil {
					return nil, err
				}
				// Every fill rechecks authority. Never fill a later close
				// before this close's safety policy has committed.
				// ponytail: full replay per fill/close grows with history;
				// optimize only with measured, equivalent recovery proof.
				for domain.PaperOrderNeedsFill((*orderdomain.State)(state)) {
					if err := r.Heartbeat(ctx); err != nil {
						return nil, err
					}
					filled := state.FilledQuantity
					state, err = s.RunPaperOrderThroughCloseWithClaim(ctx, id, r.Lease.FencingToken, r.Claim, asOf)
					if err != nil {
						return nil, err
					}
					if state.FilledQuantity == filled {
						break
					}
				}
			}
		}
		if err := r.Heartbeat(ctx); err != nil {
			return nil, err
		}
		// Even a cached policy must pass the current-selection C3 guard.
		result.Policy, err = s.RunPaperPerformancePolicyAtCloseWithClaim(ctx, account, asOf, r.Claim)
		if err != nil {
			return nil, err
		}
		if domain.PaperPolicyStopsExecution(result.Policy.Decision) {
			return result, nil
		}
	}
	if err := r.Heartbeat(ctx); err != nil {
		return nil, err
	}
	_, order, err := s.ProcessPaperProposal(ctx, account, selection, proposalRaw, snapshot.SignalBarObservationID, r.Lease.FencingToken, true, r.Claim)
	if err != nil {
		return nil, err
	}
	result.Order = order
	return result, nil
}
func (s *UseCases) BeginLocalPaperRun(account, selection string) contracts.LocalPaperRun {
	return &LocalPaperRun{Service: s, Account: account, Selection: selection}
}
func (r *LocalPaperRun) AcceptBundle(ctx context.Context, raw []byte) (*LocalPaperStepResult, error) {
	proposal, bars, research, err := r.Service.inputs.Bundle(raw)
	if err != nil {
		return nil, err
	}
	return r.Step(ctx, proposal, bars, research)
}

func (s *UseCases) checkLocalLease(ctx context.Context, account string, fence int64) error {
	return s.journal.Inspect(ctx, func(tx InspectionScope) error {
		state, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, account)
		if err != nil {
			return err
		}
		return s.ValidateOwnedExecutionLease(state.ExecutionAuthorityState, fence, s.now())
	})
}
func (s *UseCases) loadLocalOrderState(ctx context.Context, id string) (*OrderState, error) {
	return inspectResult(ctx, s.journal, func(tx InspectionScope) (*OrderState, error) { return tx.Orders().LoadOrderStateFrom(ctx, id) })
}
func (s *UseCases) loadPerformancePoint(ctx context.Context, account, asOf string) (*PaperPerformanceEvent, bool, error) {
	var found bool
	point, err := inspectResult(ctx, s.journal, func(tx InspectionScope) (*PaperPerformanceEvent, error) {
		p, f, err := tx.Evaluations().LoadPaperPerformanceByKey(ctx, account, asOf)
		found = f
		return p, err
	})
	return point, found, err
}
func (s *UseCases) LocalPaperPolicyCloses(ctx context.Context, account, latest string, committed bool) ([]string, error) {
	latest, ok := canonicalPaperTime(latest)
	if !ok {
		return nil, errors.New("local paper policy close is invalid")
	}
	return inspectResult(ctx, s.journal, func(tx InspectionScope) ([]string, error) {
		if _, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx); err != nil {
			return nil, err
		}
		if committed {
			var err error
			// Replaying an already committed signal may recover its pending order
			// against later stored bars, but final admission remains that same
			// immutable decision. A fresh proposal never gains this permission.
			latest, err = tx.Evaluations().LatestAvailableClose(ctx, s.now())
			if err != nil {
				return nil, err
			}
		}
		previous, points, err := tx.Evaluations().PolicyFrontier(ctx, account, latest)
		if err != nil {
			return nil, err
		}
		_, selection, _, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
		if err != nil {
			return nil, err
		}
		selectedAt, err := tx.Selections().RecordedSelectionTime(ctx, selection)
		if err != nil {
			return nil, err
		}
		selectedAt, ok = canonicalPaperTime(selectedAt)
		if !ok {
			return nil, errors.New("local paper selection time is invalid")
		}
		_, complete, err := tx.Evaluations().ScheduledResult(ctx, account, previous)
		if err != nil {
			return nil, err
		}
		includePrevious := domain.IncludeLocalPolicyFrontier(complete, previous, latest)
		// New C3 rows must fall inside the current selection's D window. Keep an
		// existing frontier for retry/selection validation, never invent one before
		// selection and strand an immutable point which D cannot consume.
		now := s.now().UTC().Format(canonicalPaperTimeLayout)
		closes, err := tx.Evaluations().PolicyCloses(ctx, previous, latest, selectedAt, now, includePrevious, domain.IncludeCommittedPolicyFrontier(points, includePrevious))
		if err != nil {
			return nil, err
		}
		if err := domain.ValidateLocalPolicyCloses(closes, latest); err != nil {
			return nil, err
		}
		return closes, nil
	})
}
func (s *UseCases) OpenLocalPaperOrderIDs(ctx context.Context, account string) ([]string, error) {
	return inspectResult(ctx, s.journal, func(tx InspectionScope) ([]string, error) {
		ids, err := tx.Orders().PaperOrderIDs(ctx, account)
		if err != nil {
			return nil, err
		}
		var active []string
		for _, id := range ids {
			intent, err := tx.Orders().LoadOrderIntentFrom(ctx, id)
			if err != nil {
				return nil, err
			}
			if err := domain.ValidateLocalPaperIntent(domain.OrderIntent(intent)); err != nil {
				return nil, err
			}
			state, err := tx.Orders().LoadOrderStateFrom(ctx, id)
			if err != nil {
				return nil, err
			}
			include, err := domain.LocalPaperOrderExecutable((*orderdomain.State)(state))
			if err != nil {
				return nil, err
			}
			if include {
				active = append(active, id)
			}
		}
		return active, nil
	})
}
func (s *UseCases) StartLocalPaperExecutionWithClaim(ctx context.Context, accountRef, selectionEventID string, claim *paperRunnerClaim) (*ExecutionAuthorityState, error) {
	if s == nil || s.journal == nil || s.now == nil || s.executionOwner == "" || !orderAlias(accountRef, "account") || !safeOrderID(selectionEventID) {
		return nil, errors.New("local paper execution is not configured")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*ExecutionAuthorityState, error) {
		if claim != nil {
			if err := ValidatePaperRunnerLeaseTx(ctx, tx.Runner(), claim, accountRef, s.paperRunnerOwner, s.now()); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
			return nil, err
		}
		_, currentEvent, result, err := tx.Evaluations().CurrentPaperPerformanceSelection(ctx)
		if bindErr := domain.ValidateLocalExecutionSelection(err == nil, currentEvent, result, selectionEventID); bindErr != nil {
			return nil, bindErr
		}
		session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, accountRef)
		if err != nil {
			return nil, err
		}
		policy, err := tx.Selections().LoadCurrentStrategyExecutionPolicy(ctx, result, selectionEventID)
		var sessionPolicy string
		if err == nil && found {
			sessionPolicy = session.ExecutionPolicySHA256
		}
		if bindErr := domain.ValidateLocalExecutionSession(err == nil, found, sessionPolicy, policy.SHA256); bindErr != nil {
			return nil, bindErr
		}
		if err := tx.Paper().RequireIsolatedPaperAccount(ctx, accountRef); err != nil {
			return nil, err
		}
		current, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, accountRef)
		if err != nil {
			return nil, err
		}
		if err := domain.ValidateLocalExecutionFence(current.FencingToken); err != nil {
			return nil, err
		}
		if !current.Armed {
			if err := tx.Authority().InsertExecutionAuthorityRecord(ctx, executionAuthorityRecord{
				EventID: s.id("execution_authority"), AccountRef: accountRef, Armed: true,
				FencingToken: current.FencingToken + 1, ReasonCode: "manual_arm", RecordedAt: s.now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return nil, err
			}
		}
		lease, err := s.AcquireSyntheticExecutionLeaseTx(ctx, tx, accountRef)
		if err != nil {
			return nil, err
		}
		return lease, nil
	})
}
func (s *UseCases) HaltOwnedSyntheticExecutionLease(ctx context.Context, accountRef string, fence int64) error {
	if s == nil || s.journal == nil || s.now == nil || s.executionOwner == "" || !orderAlias(accountRef, "account") || fence <= 0 {
		return errors.New("local paper cleanup is not configured")
	}
	return s.journal.Decide(ctx, func(tx WriteScope) (bool, error) {
		if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
			return false, err
		}
		current, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, accountRef)
		if err != nil {
			return false, err
		}
		if !current.Armed {
			return false, nil
		}
		if err := domain.ValidateLocalCleanupLease(domain.ExecutionAuthorityState(current.ExecutionAuthorityState), s.executionOwner, fence); err != nil {
			return false, err
		}
		if _, err := s.HaltSyntheticExecutionTx(ctx, tx, s.now().UTC(), "manual_halt", "", []string{accountRef}, nil); err != nil {
			return false, err
		}
		return true, nil
	})
}
