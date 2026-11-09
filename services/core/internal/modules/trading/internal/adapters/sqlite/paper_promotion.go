package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"

	"database/sql"

	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/researchdomain"
)

const paperPromotionClaimSchema = "paper-promotion-claim.v1"

type paperPromotionClaim = application.PromotionClaim

type paperPromotionDecision = application.PromotionDecision

type PaperPromotionReceipt = contracts.PaperPromotionReceipt

type paperPromotionRecoveryProof struct {
	SHA256            string
	Claims, Decisions int
}

var promotionReceipt = application.PromotionReceipt

func (s *Store) PromotePaperCandidate(ctx context.Context, account, candidateSHA, expectedSelection, attemptID string, raw []byte) (*PaperPromotionReceipt, error) {
	return s.business().PromotePaperCandidate(ctx, account, candidateSHA, expectedSelection, attemptID, raw)
}

func (s *Store) claimPaperPromotion(ctx context.Context, account, candidateSHA, expectedSelection, attemptID string, raw []byte) (*paperPromotionClaim, *PaperPromotionReceipt, error) {
	return s.business().ClaimPaperPromotion(ctx, account, candidateSHA, expectedSelection, attemptID, raw)
}

func paperPromotionInputs(ctx context.Context, q orderQuerier, candidateSHA, incumbentSHA, cash, policySHA string) (researchdomain.Strategy, researchdomain.Strategy, researchdomain.Execution, error) {
	var strategies [2]researchdomain.Strategy
	var policies [2]strategyExecutionPolicy
	for i, sha := range []string{candidateSHA, incumbentSHA} {
		var raw string
		if err := q.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=? AND target='paper_candidate'`, sha).Scan(&raw); err != nil {
			return strategies[0], strategies[1], researchdomain.Execution{}, err
		}
		evidence, err := decodeStrategyArtifact([]byte(raw))
		if err != nil {
			return strategies[0], strategies[1], researchdomain.Execution{}, err
		}
		strategies[i], err = paperPromotionStrategy(*evidence)
		if err != nil {
			return strategies[0], strategies[1], researchdomain.Execution{}, err
		}
		policies[i] = evidence.ExecutionPolicy
	}
	if strategies[0].Quantity != strategies[1].Quantity || policies[0].SHA256 != policies[1].SHA256 || policies[0].SHA256 != policySHA {
		return strategies[0], strategies[1], researchdomain.Execution{}, errors.New("paper promotion requires identical quantity and execution policy")
	}
	return strategies[0], strategies[1], researchdomain.Execution{StartingCash: cash, Policy: paperExecutionPolicy(policies[0]), DelayBars: int(policies[0].DelayBars)}, nil
}

func scorePaperPromotionClaim(ctx context.Context, q orderQuerier, claim paperPromotionClaim) (paperPromotionComparison, error) {
	_, bars, err := parsePaperPromotionBars(ctx, []byte(claim.InputCSV))
	if err != nil {
		return paperPromotionComparison{}, err
	}
	candidate, incumbent, execution, err := paperPromotionInputs(ctx, q, claim.CandidateResultSHA256, claim.IncumbentResultSHA256, claim.StartingCash, claim.ExecutionPolicySHA256)
	if err != nil {
		return paperPromotionComparison{}, err
	}
	return comparePaperPromotion(ctx, bars, candidate, incumbent, execution)
}

func (s *Store) finishPaperPromotion(ctx context.Context, claim paperPromotionClaim, comparison paperPromotionComparison, scoreErr error, abort bool) (*PaperPromotionReceipt, error) {
	return s.business().FinishPaperPromotion(ctx, claim, comparison, scoreErr, abort)
}

func (s *Store) PaperPromotionStatus(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error) {
	if !safeOrderID(attemptID) {
		return nil, errors.New("paper promotion attempt ID is invalid")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireSchemaContext(ctx, tx); err != nil {
		return nil, err
	}
	if _, err := replayStrategyRegistry(ctx, tx); err != nil {
		return nil, err
	}
	claim, decision, found, err := loadPaperPromotion(ctx, tx, attemptID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("paper promotion attempt does not exist")
	}
	return promotionReceipt(*claim, decision), nil
}

func (s *Store) AbortPaperPromotion(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error) {
	if !safeOrderID(attemptID) {
		return nil, errors.New("paper promotion attempt ID is invalid")
	}
	claim, _, found, err := loadPaperPromotion(ctx, s.db, attemptID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("paper promotion attempt does not exist")
	}
	return s.finishPaperPromotion(ctx, *claim, paperPromotionComparison{}, nil, true)
}

func loadPaperPromotion(ctx context.Context, q orderQuerier, id string) (*paperPromotionClaim, *paperPromotionDecision, bool, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT record_json FROM paper_promotion_claims WHERE attempt_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	var claim paperPromotionClaim
	if err := json.Unmarshal([]byte(raw), &claim); err != nil {
		return nil, nil, false, err
	}
	err = q.QueryRowContext(ctx, `SELECT record_json FROM paper_promotion_decisions WHERE attempt_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return &claim, nil, true, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	var decision paperPromotionDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return nil, nil, false, err
	}
	return &claim, &decision, true, nil
}

func validatePaperPromotionFreshRange(ctx context.Context, q orderQuerier, claim paperPromotionClaim, sequence int64) error {
	start, ok := paperResearchWholeSecond(claim.ComparisonStart)
	end, endOK := paperResearchWholeSecond(claim.ComparisonEnd)
	if !ok || !endOK || end.Before(start) {
		return errors.New("paper promotion interval is invalid")
	}
	rows, err := q.QueryContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE sequence<=? ORDER BY sequence`, claim.ResearchSequenceCutoff)
	if err != nil {
		return err
	}
	var artifacts []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		artifacts = append(artifacts, raw)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, raw := range artifacts {
		var artifact struct {
			Evaluation struct {
				Folds        []struct{ Train, Validation map[string]any }
				FinalHoldout map[string]any `json:"final_holdout"`
			}
		}
		if json.Unmarshal([]byte(raw), &artifact) != nil || len(artifact.Evaluation.Folds) < 2 {
			return errors.New("registered research interval cannot be verified")
		}
		periods := []map[string]any{artifact.Evaluation.FinalHoldout}
		for _, fold := range artifact.Evaluation.Folds {
			periods = append(periods, fold.Train, fold.Validation)
		}
		for _, period := range periods {
			first, firstOK := paperResearchWholeSecond(stringField(period, "first_bar_at"))
			last, lastOK := paperResearchWholeSecond(stringField(period, "last_bar_at"))
			if !firstOK || !lastOK || last.Before(first) {
				return errors.New("registered research interval cannot be verified")
			}
			if !start.After(last) {
				return errors.New("paper comparison must follow all registered research data")
			}
		}
	}
	var overlap int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM paper_promotion_claims WHERE (?=0 OR sequence<?) AND json_extract(record_json,'$.comparison_end')>=?`, sequence, sequence, claim.ComparisonStart).Scan(&overlap); err != nil {
		return err
	}
	if overlap != 0 {
		return errors.New("paper comparison interval has already been consumed")
	}
	return nil
}

func validatePaperPromotionSignalCutoff(ctx context.Context, q orderQuerier, result, selectionID, asOf string) error {
	var schema int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&schema); err != nil {
		return err
	}
	if schema < 27 {
		return nil
	}
	var cutoff sql.NullString
	err := q.QueryRowContext(ctx, `SELECT MAX(json_extract(c.record_json,'$.comparison_end')) FROM paper_promotion_decisions d
		JOIN paper_promotion_claims c ON c.attempt_id=d.attempt_id JOIN strategy_selection_events s ON s.event_id=d.selection_event_id
		WHERE s.selected_result_sha256=? AND s.sequence<=(SELECT sequence FROM strategy_selection_events WHERE event_id=?)`, result, selectionID).Scan(&cutoff)
	if err != nil {
		return err
	}
	if cutoff.Valid {
		at, ok := parsePaperTime(asOf)
		end, endOK := paperResearchWholeSecond(cutoff.String)
		if !ok || !endOK || !at.After(end) {
			return fmt.Errorf("paper signal must follow the automatic comparison interval")
		}
	}
	return nil
}
