package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
)

func (s *UseCases) RecordOrderIntent(ctx context.Context, intent OrderIntent) (*OrderState, error) {
	return writeResult(ctx, s.journal, func(scope WriteScope) (*OrderState, error) { return s.RecordOrderIntentTx(ctx, scope, intent) })
}
func (s *UseCases) RecordOrderIntentTx(ctx context.Context, tx WriteScope, intent OrderIntent) (*OrderState, error) {
	if err := domain.ValidateOrderIntent(domain.OrderIntent(intent)); err != nil {
		return nil, err
	}
	_, requestSHA, err := orderJSONHash(intent)
	if err != nil {
		return nil, err
	}
	priorSHA, priorOrderID, found, err := tx.Orders().FindIntentKey(ctx, intent)
	if err != nil {
		return nil, err
	}
	if found {
		if priorSHA != requestSHA {
			return nil, errors.New("client_order_id was already used with a different intent")
		}
		return tx.Orders().LoadOrderStateFrom(ctx, priorOrderID)
	}
	if intent.Mode == "paper" && intent.SignalSchemaVersion != "paper-signal.v3" {
		schema, err := tx.Recovery().SchemaVersion(ctx)
		if err != nil {
			return nil, err
		}
		if schema >= 17 {
			return nil, errors.New("new paper orders require the target-based signal schema")
		}
	}
	if err := tx.Selections().ValidateStrategyOrderSelection(ctx, intent); err != nil {
		return nil, err
	}
	orderID := s.id("order")
	recordedAt := s.now().UTC().Format(time.RFC3339Nano)
	if err := tx.Orders().RecordIntent(ctx, intent, orderID, recordedAt); err != nil {
		return nil, err
	}
	event := OrderEvent{EventID: s.id("order_event"), OrderID: orderID, Type: "INTENT_RECORDED", Source: "synthetic"}
	if err := tx.Orders().InsertOrderEvent(ctx, event, recordedAt); err != nil {
		return nil, err
	}
	return ApplyOrderEvent(NewOrderState(orderID, intent), event)
}
func NewOrderState(orderID string, intent OrderIntent) *OrderState {
	return (*OrderState)(orderdomain.NewState(orderID, intent.ClientOrderID, intent.AccountRef, intent.Quantity, intent.LimitPrice))
}
func ApplyOrderEvent(current *OrderState, event OrderEvent) (*OrderState, error) {
	next, err := orderdomain.Transition((*orderdomain.State)(current), orderdomain.Event(event))
	return (*OrderState)(next), err
}
func (s *UseCases) AppendOrderEvent(ctx context.Context, event OrderEvent) (*OrderState, error) {
	if err := ValidateOrderEvent(event); err != nil {
		return nil, err
	}
	return writeResult(ctx, s.journal, func(scope WriteScope) (*OrderState, error) {
		return scope.Orders().AppendOrderEventTx(ctx, event, s.now().UTC().Format(time.RFC3339Nano))
	})
}
func (s *UseCases) SetSyntheticExecutionArmed(ctx context.Context, account string, armed bool) (*ExecutionAuthorityState, error) {
	if !orderAlias(account, "account") {
		return nil, errors.New("execution account reference is invalid")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*ExecutionAuthorityState, error) {
		if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
			return nil, fmt.Errorf("execution authority policy recovery: %w", err)
		}
		current, err := tx.Authority().LoadExecutionAuthoritySnapshot(ctx, account)
		if err != nil {
			return nil, err
		}
		if current.Armed == armed {
			state := current.ExecutionAuthorityState
			return &state, nil
		}
		now := s.now().UTC()
		reason := "manual_halt"
		if armed {
			reason = "manual_arm"
		}
		record := AuthorityRecord{EventID: s.id("execution_authority"), AccountRef: account, Armed: armed, FencingToken: current.FencingToken + 1, ReasonCode: reason, RecordedAt: now.Format(time.RFC3339Nano)}
		if err := tx.Authority().InsertExecutionAuthorityRecord(ctx, record); err != nil {
			return nil, err
		}
		return AuthorityState(record), nil
	})
}
func (s *UseCases) AcquireSyntheticExecutionLease(ctx context.Context, account string) (*ExecutionAuthorityState, error) {
	if !orderAlias(account, "account") {
		return nil, errors.New("execution account reference is invalid")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*ExecutionAuthorityState, error) {
		return s.AcquireSyntheticExecutionLeaseTx(ctx, tx, account)
	})
}

type DispatchAuthorization struct {
	State      *OrderState
	Dispatched bool
}

func (s *UseCases) AuthorizeSyntheticDispatchOnce(ctx context.Context, id string, fence int64) (*OrderState, bool, error) {
	result, err := writeResult(ctx, s.journal, func(tx WriteScope) (DispatchAuthorization, error) {
		if _, _, err := tx.Recovery().ProveAuthorityRecovery(ctx); err != nil {
			return DispatchAuthorization{}, fmt.Errorf("execution authority recovery: %w", err)
		}
		state, dispatched, err := s.AuthorizeSyntheticDispatchOnceTx(ctx, tx, id, fence)
		return DispatchAuthorization{state, dispatched}, err
	})
	if err != nil {
		return nil, false, err
	}
	return result.State, result.Dispatched, nil
}
func (s *UseCases) AuthorizeSyntheticDispatch(ctx context.Context, id string, fence int64) (*OrderState, error) {
	state, _, err := s.AuthorizeSyntheticDispatchOnce(ctx, id, fence)
	return state, err
}
func (s *UseCases) RegisterEvidenceRecord(ctx context.Context, artifact []byte) (*Evidence, error) {
	evidence, err := DecodeStrategyArtifact(artifact)
	if err != nil {
		return nil, err
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*Evidence, error) {
		if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
			return nil, fmt.Errorf("strategy evidence policy recovery: %w", err)
		}
		saved, recordedAt, found, err := tx.Selections().FindEvidence(ctx, evidence.ResultSHA256)
		if err != nil {
			return nil, err
		}
		if found {
			if saved != evidence.ArtifactJSON {
				return nil, errors.New("strategy result hash is already bound to different evidence")
			}
			evidence.RecordedAt = recordedAt
			return evidence, nil
		}
		evidence.RecordedAt = s.now().UTC().Format(time.RFC3339Nano)
		if err := tx.Selections().RequirePromotionAppendTime(ctx, evidence.RecordedAt); err != nil {
			return nil, err
		}
		if err := tx.Selections().RecordEvidence(ctx, *evidence); err != nil {
			return nil, err
		}
		return evidence, nil
	})
}
func (s *UseCases) RegisterEvidence(ctx context.Context, artifact []byte) (*contracts.StrategyEvidence, error) {
	e, err := s.RegisterEvidenceRecord(ctx, artifact)
	if err != nil {
		return nil, err
	}
	result := e.StrategyEvidence
	return &result, nil
}
func (s *UseCases) SelectPaperCandidate(ctx context.Context, result, expected string) (*StrategySelectionState, error) {
	if !strategySHA256Pattern.MatchString(result) || !safeOrderID(expected) {
		return nil, errors.New("strategy selection identifiers are invalid")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*StrategySelectionState, error) {
		if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
			return nil, fmt.Errorf("strategy selection policy recovery: %w", err)
		}
		now := s.now().UTC()
		if err := tx.Runner().RejectLiveLease(ctx, now); err != nil {
			return nil, err
		}
		state, err := tx.Selections().ReplayStrategyRegistry(ctx)
		if err != nil {
			return nil, err
		}
		if err := domain.ValidateExpectedSelection(state.CurrentEventID, expected); err != nil {
			return nil, err
		}
		target, found, err := tx.Selections().EvidenceTarget(ctx, result)
		if err != nil {
			return nil, err
		}
		if err := domain.ValidatePaperCandidateSelection(found, target, state.SelectedResultSHA256, result); err != nil {
			return nil, err
		}
		event := StrategySelectionEvent{EventID: s.id("strategy_selection"), EventType: "SELECT", CandidateResultSHA256: result, ExpectedCurrentEventID: expected, PreviousSelectedResultSHA256: state.SelectedResultSHA256, SelectedResultSHA256: result, ReasonCode: "manual_selection", RecordedAt: now.Format(time.RFC3339Nano)}
		event.PaperEvaluationSequence, err = tx.Selections().LatestPaperEvaluationSequence(ctx)
		if err != nil {
			return nil, err
		}
		if err := tx.Selections().InsertStrategySelectionEvent(ctx, event); err != nil {
			return nil, err
		}
		return &StrategySelectionState{CurrentEventID: event.EventID, SelectedResultSHA256: result}, nil
	})
}
func (s *UseCases) RollbackPaperCandidate(ctx context.Context, expected, source string) (*StrategySelectionState, error) {
	if !safeOrderID(expected) || !safeOrderID(source) {
		return nil, errors.New("strategy rollback identifiers are invalid")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*StrategySelectionState, error) {
		if _, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx); err != nil {
			return nil, fmt.Errorf("strategy rollback policy recovery: %w", err)
		}
		now := s.now().UTC()
		if err := tx.Runner().RejectLiveLease(ctx, now); err != nil {
			return nil, err
		}
		state, err := tx.Selections().ReplayStrategyRegistry(ctx)
		if err != nil {
			return nil, err
		}
		if err := domain.ValidateRollbackSource(state.CurrentEventID, expected, source); err != nil {
			return nil, err
		}
		armed, err := tx.Authority().ArmedExecutionAccounts(ctx)
		if err != nil {
			return nil, err
		}
		if _, err := s.HaltSyntheticExecutionTx(ctx, tx, now, "manual_halt", "", armed, nil); err != nil {
			return nil, err
		}
		return s.RollbackPaperCandidateTx(ctx, tx, state, expected, source, "manual_rollback", "", s.id("strategy_selection"), now)
	})
}
func (s *UseCases) OpenPaperAccountingSessionChecked(ctx context.Context, account, result, selection string, isolated bool) (*PaperAccountingSession, error) {
	if s == nil || s.journal == nil || !orderAlias(account, "account") || !strategySHA256Pattern.MatchString(result) || !safeOrderID(selection) {
		return nil, errors.New("paper accounting session identity is invalid")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*PaperAccountingSession, error) {
		if _, err := tx.Recovery().ProveOrderRecovery(ctx); err != nil {
			return nil, fmt.Errorf("paper accounting order recovery: %w", err)
		}
		if _, err := tx.Selections().ReplayStrategyRegistry(ctx); err != nil {
			return nil, fmt.Errorf("paper accounting strategy recovery: %w", err)
		}
		if isolated {
			if err := tx.Paper().RequireIsolatedPaperAccount(ctx, account); err != nil {
				return nil, err
			}
		}
		existing, found, err := tx.Paper().LoadPaperAccountingSession(ctx, account)
		if err != nil {
			return nil, err
		}
		if found {
			if existing.StrategyResultSHA256 != result || existing.StrategySelectionEventID != selection {
				return nil, errors.New("paper accounting session is already bound to different initial evidence")
			}
			return existing, nil
		}
		policy, err := tx.Selections().LoadCurrentStrategyExecutionPolicy(ctx, result, selection)
		if err != nil {
			return nil, err
		}
		count, err := tx.Paper().PaperOrderCount(ctx, account)
		if err != nil {
			return nil, err
		}
		if count != 0 {
			return nil, errors.New("paper accounting session cannot follow a paper order")
		}
		session := PaperAccountingSession{SchemaVersion: "paper-accounting-session.v1", AccountRef: account, StrategyResultSHA256: result, StrategySelectionEventID: selection, ExecutionPolicySHA256: policy.SHA256, ExecutionPolicyJSON: policy.CanonicalJSON, StartingCash: policy.StartingCash, Currency: "KRW", RecordedAt: s.now().UTC().Format(time.RFC3339Nano)}
		session.SessionID = PaperAccountingSessionID(account, result, selection, policy.SHA256)
		if err := tx.Paper().InsertAccountingSession(ctx, session); err != nil {
			return nil, err
		}
		return &session, nil
	})
}
func (s *UseCases) OpenPaperAccountingSession(ctx context.Context, account, result, selection string) (*PaperAccountingSession, error) {
	return s.OpenPaperAccountingSessionChecked(ctx, account, result, selection, false)
}
func (s *UseCases) InitializePaperAccount(ctx context.Context, account, result, selection string) (*PaperAccountingSession, error) {
	return s.OpenPaperAccountingSessionChecked(ctx, account, result, selection, true)
}

var canonicalUTCString = domain.CanonicalUTCString

var PaperAccountingSessionID = domain.AccountingSessionID
