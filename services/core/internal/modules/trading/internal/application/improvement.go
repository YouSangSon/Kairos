package application

import (
	"context"
	"errors"
)

func (s *UseCases) ImprovePaperCandidate(ctx context.Context, account, attemptID string, artifact, raw []byte) (*PaperImprovementReceipt, error) {
	if ctx == nil {
		return nil, errors.New("paper improvement context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.journal == nil || !orderAlias(account, "account") || !safeOrderID(attemptID) {
		return nil, errors.New("paper improvement identifiers are invalid")
	}
	evidence, err := DecodeStrategyArtifact(artifact)
	if err != nil {
		return nil, err
	}
	if evidence.Target == "paper_candidate" {
		if _, err := PaperPromotionStrategy(*evidence); err != nil {
			return nil, err
		}
	}
	if _, _, err := s.inputs.Promotion(ctx, raw); err != nil {
		return nil, err
	}
	var selection StrategyRegistryProof
	var found bool
	var expectedSelection string
	err = s.promotionReader.ImprovementContext(ctx, attemptID, func(state StrategyRegistryProof, claim *PromotionClaim, exists bool) error {
		selection, found = state, exists
		expectedSelection = state.CurrentEventID
		if exists {
			if claim.AccountRef != account || claim.CandidateResultSHA256 != evidence.ResultSHA256 || claim.InputCSV != string(raw) {
				return errors.New("paper improvement attempt conflicts with its saved request")
			}
			expectedSelection = claim.ExpectedCurrentEventID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Registration is independently durable; only a claim binds the attempt ID.
	if _, err := s.RegisterEvidenceRecord(ctx, artifact); err != nil {
		return nil, err
	}
	receipt := &PaperImprovementReceipt{SchemaVersion: "paper-improvement.v1", ResultSHA256: evidence.ResultSHA256, Registered: true, Disposition: "comparison"}
	if !found {
		if evidence.Target == "no_promotion" {
			receipt.Disposition = "research_not_candidate"
			return receipt, nil
		}
		if selection.SelectedResultSHA256 == evidence.ResultSHA256 {
			receipt.Disposition = "already_selected"
			return receipt, nil
		}
	}
	receipt.Comparison, err = s.PromotePaperCandidate(ctx, account, evidence.ResultSHA256, expectedSelection, attemptID, raw)
	if err != nil {
		return nil, err
	}
	receipt.AttemptRecorded = true
	return receipt, nil
}
