package domain

import (
	"context"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/exact"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
)

const paperPromotionPolicyVersion = "paper-forward-comparison.v1"

type PaperPromotionWindow struct {
	Start      string                 `json:"start"`
	End        string                 `json:"end"`
	Candidate  researchdomain.Metrics `json:"candidate"`
	Incumbent  researchdomain.Metrics `json:"incumbent"`
	BuyAndHold researchdomain.Metrics `json:"buy_and_hold"`
}

type PaperPromotionComparison struct {
	PolicyVersion string                 `json:"policy_version"`
	Windows       []PaperPromotionWindow `json:"windows"`
	Decision      string                 `json:"decision"`
	Reason        string                 `json:"reason"`
}

func ComparePaperPromotion(ctx context.Context, bars []researchdomain.Bar, candidate, incumbent researchdomain.Strategy, execution researchdomain.Execution) (PaperPromotionComparison, error) {
	if err := ctx.Err(); err != nil {
		return PaperPromotionComparison{}, err
	}
	for _, strategy := range []researchdomain.Strategy{candidate, incumbent} {
		if strategy.FastWindow <= 0 || strategy.SlowWindow <= strategy.FastWindow || strategy.SlowWindow > 365 || !paperdomain.ValidCapitalizedQuantity(strategy.Quantity, false) {
			return PaperPromotionComparison{}, errors.New("paper promotion strategy is outside the paper SMA bounds")
		}
	}
	if candidate.Quantity != incumbent.Quantity {
		return PaperPromotionComparison{}, errors.New("paper promotion strategies must use identical quantities")
	}
	commonWarmup := int(max(candidate.SlowWindow, incumbent.SlowWindow))
	if len(bars) != commonWarmup+3*30 {
		return PaperPromotionComparison{}, errors.New("paper promotion requires exactly the common SMA warmup and three 30-bar windows")
	}
	comparison := PaperPromotionComparison{PolicyVersion: paperPromotionPolicyVersion, Windows: make([]PaperPromotionWindow, 0, 3)}
	for index := range 3 {
		start := commonWarmup + index*30
		windowBars, warmup := bars[start:start+30], bars[start-commonWarmup:start]
		window := PaperPromotionWindow{Start: windowBars[0].At, End: windowBars[len(windowBars)-1].At}
		var err error
		var metrics researchdomain.Metrics
		metrics, _, err = researchdomain.SimulateFold(ctx, windowBars, warmup, candidate, execution, false)
		if err != nil {
			return PaperPromotionComparison{}, fmt.Errorf("paper promotion window %d candidate: %w", index+1, err)
		}
		window.Candidate = researchdomain.Metrics(metrics)
		metrics, _, err = researchdomain.SimulateFold(ctx, windowBars, warmup, incumbent, execution, false)
		if err != nil {
			return PaperPromotionComparison{}, fmt.Errorf("paper promotion window %d incumbent: %w", index+1, err)
		}
		window.Incumbent = researchdomain.Metrics(metrics)
		metrics, _, err = researchdomain.SimulateFold(ctx, windowBars, warmup, candidate, execution, true)
		if err != nil {
			return PaperPromotionComparison{}, fmt.Errorf("paper promotion window %d buy-and-hold: %w", index+1, err)
		}
		window.BuyAndHold = researchdomain.Metrics(metrics)
		comparison.Windows = append(comparison.Windows, window)
	}
	var err error
	comparison.Decision, comparison.Reason, err = EvaluatePaperPromotionWindows(comparison.Windows)
	if err != nil {
		return PaperPromotionComparison{}, err
	}
	return comparison, nil
}

func EvaluatePaperPromotionWindows(windows []PaperPromotionWindow) (string, string, error) {
	if len(windows) != 3 {
		return "", "", errors.New("paper promotion decision requires three windows")
	}
	for _, window := range windows {
		for _, trades := range []string{window.Candidate.TradeCount, window.Incumbent.TradeCount} {
			order, err := PaperPromotionMetricOrder(trades, "1")
			if err != nil {
				return "", "", err
			}
			if order < 0 {
				return "HOLD", "insufficient_round_trips", nil
			}
		}
		for _, gate := range []struct {
			candidate, bound, reason string
			rejectedOrder            int
		}{
			{window.Candidate.AfterCostReturn, "0", "candidate_negative_return", -1},
			{window.Candidate.AfterCostReturn, window.Incumbent.AfterCostReturn, "candidate_return_below_incumbent", -1},
			{window.Candidate.MaxDrawdown, window.Incumbent.MaxDrawdown, "candidate_drawdown_above_incumbent", 1},
			{window.Candidate.Turnover, window.Incumbent.Turnover, "candidate_turnover_above_incumbent", 1},
		} {
			order, err := PaperPromotionMetricOrder(gate.candidate, gate.bound)
			if err != nil {
				return "", "", err
			}
			if order == gate.rejectedOrder {
				return "HOLD", gate.reason, nil
			}
		}
	}
	holdout := windows[2]
	for _, gate := range []struct{ bound, reason string }{
		{holdout.Incumbent.AfterCostReturn, "holdout_not_better_than_incumbent"},
		{holdout.BuyAndHold.AfterCostReturn, "holdout_not_better_than_buy_and_hold"},
	} {
		order, err := PaperPromotionMetricOrder(holdout.Candidate.AfterCostReturn, gate.bound)
		if err != nil {
			return "", "", err
		}
		if order <= 0 {
			return "HOLD", gate.reason, nil
		}
	}
	return "PROMOTE", "all_forward_gates_passed", nil
}

func PaperPromotionMetricOrder(left, right string) (int, error) {
	l, err := exact.ParseDecimal(left)
	if err != nil {
		return 0, err
	}
	r, err := exact.ParseDecimal(right)
	if err != nil {
		return 0, err
	}
	return l.Cmp(r), nil
}
