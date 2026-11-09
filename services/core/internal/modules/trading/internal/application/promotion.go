package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"omni-folio/services/core/internal/researchdomain"
	"reflect"
	"time"
)

func (s *UseCases) ClaimPaperPromotion(ctx context.Context, account, candidateSHA, expectedSelection, attemptID string, raw []byte) (*PromotionClaim, *PaperPromotionReceipt, error) {
	if s == nil || s.journal == nil || ctx == nil || !orderAlias(account, "account") || !strategySHA256Pattern.MatchString(candidateSHA) || !safeOrderID(expectedSelection) || !safeOrderID(attemptID) {
		return nil, nil, errors.New("paper promotion identifiers are invalid")
	}
	symbol, bars, err := ParsePaperPromotionBars(ctx, raw)
	if err != nil {
		return nil, nil, err
	}
	var claim *PromotionClaim
	var prior *PaperPromotionReceipt
	err = s.journal.Write(ctx, func(tx WriteScope) error {
		var err error
		claim, prior, err = s.claimPaperPromotionInScope(ctx, tx, account, candidateSHA, expectedSelection, attemptID, raw, symbol, bars)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return claim, prior, nil
}

func (s *UseCases) claimPaperPromotionInScope(ctx context.Context, tx WriteScope, account, candidateSHA, expectedSelection, attemptID string, raw []byte, symbol string, bars []researchdomain.Bar) (*PromotionClaim, *PaperPromotionReceipt, error) {
	if err := tx.Recovery().RequireSchema(ctx); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Recovery().ProvePaperPromotionRecovery(ctx); err != nil {
		return nil, nil, err
	}
	existing, decision, found, err := tx.Promotions().LoadPaperPromotion(ctx, attemptID)
	if err != nil {
		return nil, nil, err
	}
	if found {
		if existing.AccountRef != account || existing.CandidateResultSHA256 != candidateSHA || existing.ExpectedCurrentEventID != expectedSelection || existing.InputCSV != string(raw) {
			return nil, nil, errors.New("paper promotion attempt conflicts with its saved request")
		}
		return nil, PromotionReceipt(*existing, decision), nil
	}
	var pending int
	pending, err = tx.Promotions().PendingPromotionCount(ctx)
	if err != nil {
		return nil, nil, err
	}
	if pending != 0 {
		return nil, nil, errors.New("a paper comparison is pending; inspect or explicitly abort it first")
	}
	now := s.now().UTC()
	stateSHA, session, cash, selection, err := tx.Promotions().CapturePromotionState(ctx, account, now)
	if err != nil {
		return nil, nil, err
	}
	if selection.CurrentEventID != expectedSelection || selection.SelectedResultSHA256 == candidateSHA || selection.SelectedResultSHA256 == noStrategySelection {
		return nil, nil, errors.New("paper promotion requires a distinct candidate and the current incumbent")
	}
	candidate, incumbent, _, err := tx.Promotions().PromotionInputs(ctx, candidateSHA, selection.SelectedResultSHA256, cash, session.ExecutionPolicySHA256)
	if err != nil {
		return nil, nil, err
	}
	warmup := int(max(candidate.SlowWindow, incumbent.SlowWindow))
	if len(bars) != warmup+90 {
		return nil, nil, errors.New("paper promotion requires common warmup and three fresh 30-bar windows")
	}
	end, _ := PaperResearchWholeSecond(bars[len(bars)-1].At)
	if end.After(now) {
		return nil, nil, errors.New("paper promotion input contains future bars")
	}
	hash := sha256.Sum256(raw)
	claim := PromotionClaim{SchemaVersion: paperPromotionClaimSchema, AttemptID: attemptID, ExpectedPreviousAttemptID: "no_attempt",
		AccountRef: account, CandidateResultSHA256: candidateSHA, IncumbentResultSHA256: selection.SelectedResultSHA256, ExpectedCurrentEventID: expectedSelection,
		InputDataSHA256: hex.EncodeToString(hash[:]), InputCSV: string(raw), Symbol: symbol, ComparisonStart: bars[warmup].At, ComparisonEnd: bars[len(bars)-1].At,
		StartingCash: cash, SessionID: session.SessionID, ExecutionPolicySHA256: session.ExecutionPolicySHA256, StateSHA256: stateSHA, RecordedAt: now.Format(canonicalPaperTimeLayout)}
	claim.OrderSequenceCutoff, err = tx.Promotions().OrderSequence(ctx)
	if err != nil {
		return nil, nil, err
	}
	claim.ResearchSequenceCutoff, err = tx.Promotions().ResearchSequence(ctx)
	if err != nil {
		return nil, nil, err
	}
	claim.ExpectedPreviousAttemptID, err = tx.Promotions().PreviousPromotionAttempt(ctx)
	if err != nil {
		return nil, nil, err
	}
	if claim.ExpectedPreviousAttemptID != "no_attempt" {
		var previousAt string
		previousAt, err = tx.Promotions().PromotionDecisionTime(ctx, claim.ExpectedPreviousAttemptID)
		if err != nil {
			return nil, nil, err
		}
		previous, ok := parsePaperTime(previousAt)
		if !ok || now.Before(previous) {
			return nil, nil, errors.New("paper promotion clock regressed")
		}
	}
	if err := tx.Promotions().ValidatePromotionClaim(ctx, claim, 0); err != nil {
		return nil, nil, err
	}
	if err := tx.Promotions().InsertPromotionClaim(ctx, claim); err != nil {
		return nil, nil, err
	}
	return &claim, nil, nil
}

func (s *UseCases) FinishPaperPromotion(ctx context.Context, claim PromotionClaim, comparison paperPromotionComparison, scoreErr error, abort bool) (*PaperPromotionReceipt, error) {
	var result *PaperPromotionReceipt
	err := s.journal.Write(ctx, func(tx WriteScope) error {
		var err error
		result, err = s.finishPaperPromotionInScope(ctx, tx, claim, comparison, scoreErr, abort)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *UseCases) finishPaperPromotionInScope(ctx context.Context, tx WriteScope, claim PromotionClaim, comparison paperPromotionComparison, scoreErr error, abort bool) (*PaperPromotionReceipt, error) {
	if _, err := tx.Recovery().ProvePaperPromotionRecovery(ctx); err != nil {
		return nil, err
	}
	saved, prior, found, err := tx.Promotions().LoadPaperPromotion(ctx, claim.AttemptID)
	if err != nil {
		return nil, err
	}
	if !found || !reflect.DeepEqual(*saved, claim) {
		return nil, errors.New("paper promotion claim changed")
	}
	if prior != nil {
		return PromotionReceipt(claim, prior), nil
	}
	_, claimSHA, err := orderJSONHash(claim)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	claimedAt, _ := parsePaperTime(claim.RecordedAt)
	if now.Before(claimedAt) {
		return nil, errors.New("paper promotion clock regressed")
	}
	decision := PromotionDecision{AttemptID: claim.AttemptID, ClaimSHA256: claimSHA, CandidateResultSHA256: claim.CandidateResultSHA256, ExpectedCurrentEventID: claim.ExpectedCurrentEventID, RecordedAt: now.Format(canonicalPaperTimeLayout)}
	switch {
	case abort:
		decision.Decision, decision.Reason = "ABORTED", "explicit_abort"
	case scoreErr != nil:
		decision.Decision, decision.Reason = "ERROR", "comparison_failed"
	default:
		decision.Comparison = &comparison
		decision.Decision, decision.Reason = comparison.Decision, comparison.Reason
		stateSHA, _, _, selection, stateErr := tx.Promotions().CapturePromotionState(ctx, claim.AccountRef, now)
		if stateErr != nil || stateSHA != claim.StateSHA256 || selection.CurrentEventID != claim.ExpectedCurrentEventID {
			decision.Decision, decision.Reason = "STALE", "account_or_selection_changed"
		}
	}
	var event StrategySelectionEvent
	if decision.Decision == "PROMOTE" {
		sequence, err := tx.Selections().LatestPaperEvaluationSequence(ctx)
		if err != nil {
			return nil, err
		}
		decision.SelectionEventID = s.id("strategy_selection")
		event = StrategySelectionEvent{EventID: decision.SelectionEventID, EventType: "SELECT", CandidateResultSHA256: claim.CandidateResultSHA256,
			ExpectedCurrentEventID: claim.ExpectedCurrentEventID, PreviousSelectedResultSHA256: claim.IncumbentResultSHA256, SelectedResultSHA256: claim.CandidateResultSHA256,
			ReasonCode: "auto_comparison", PaperEvaluationSequence: sequence, PaperPromotionAttemptID: claim.AttemptID, RecordedAt: now.Format(time.RFC3339Nano)}
	}
	if err := tx.Promotions().InsertPromotionDecision(ctx, decision); err != nil {
		return nil, err
	}
	if decision.SelectionEventID != "" {
		if err := tx.Selections().InsertStrategySelectionEvent(ctx, event); err != nil {
			return nil, err
		}
	}
	return PromotionReceipt(claim, &decision), nil
}

func (s *UseCases) PromotePaperCandidate(ctx context.Context, account, candidateSHA, expectedSelection, attemptID string, raw []byte) (*PaperPromotionReceipt, error) {
	claim, prior, err := s.ClaimPaperPromotion(ctx, account, candidateSHA, expectedSelection, attemptID, raw)
	if err != nil || prior != nil {
		return prior, err
	}
	comparison, scoreErr := s.scorePaperPromotionClaim(ctx, *claim)
	if ctx.Err() != nil {
		scoreErr = ctx.Err()
	}
	// A cancelled scorer must still consume its interval. A process crash leaves PENDING.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	receipt, err := s.FinishPaperPromotion(finishCtx, *claim, comparison, scoreErr, false)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return receipt, ctx.Err()
	}
	return receipt, nil
}

func (s *UseCases) scorePaperPromotionClaim(ctx context.Context, claim PromotionClaim) (paperPromotionComparison, error) {
	_, bars, err := ParsePaperPromotionBars(ctx, []byte(claim.InputCSV))
	if err != nil {
		return paperPromotionComparison{}, err
	}
	candidate, incumbent, execution, err := s.promotionReader.PromotionInputs(ctx, claim.CandidateResultSHA256, claim.IncumbentResultSHA256, claim.StartingCash, claim.ExecutionPolicySHA256)
	if err != nil {
		return paperPromotionComparison{}, err
	}
	return ComparePaperPromotion(ctx, bars, candidate, incumbent, execution)
}
