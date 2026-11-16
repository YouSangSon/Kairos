package application

import (
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
)

func PromotionWindowToDomain(w paperPromotionWindow) domain.PaperPromotionWindow {
	return domain.PaperPromotionWindow{Start: w.Start, End: w.End, Candidate: researchdomain.Metrics(w.Candidate), Incumbent: researchdomain.Metrics(w.Incumbent), BuyAndHold: researchdomain.Metrics(w.BuyAndHold)}
}
func PromotionComparisonFromDomain(c domain.PaperPromotionComparison) paperPromotionComparison {
	var windows []paperPromotionWindow
	if c.Windows != nil {
		windows = make([]paperPromotionWindow, len(c.Windows))
	}
	for i, w := range c.Windows {
		windows[i] = paperPromotionWindow{Start: w.Start, End: w.End, Candidate: contracts.PromotionMetrics(w.Candidate), Incumbent: contracts.PromotionMetrics(w.Incumbent), BuyAndHold: contracts.PromotionMetrics(w.BuyAndHold)}
	}
	return paperPromotionComparison{PolicyVersion: c.PolicyVersion, Windows: windows, Decision: c.Decision, Reason: c.Reason}
}
