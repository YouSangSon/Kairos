package sqlite

import (
	"context"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

type PaperPromotionReportView = contracts.PaperPromotionReportView
type StoredPaperPromotionReport = contracts.StoredPaperPromotionReport
type PaperPromotionReportStrategy = contracts.PaperPromotionReportStrategy
type PaperPromotionReportComparison = contracts.PaperPromotionReportComparison
type PaperPromotionReportWindow = contracts.PaperPromotionReportWindow
type PaperPromotionReportMetrics = contracts.PaperPromotionReportMetrics

var projectPaperPromotionReportMetrics = application.ProjectPaperPromotionReportMetrics

func (s *Store) PaperPromotionReport(ctx context.Context) (*PaperPromotionReportView, error) {
	return s.business().PaperPromotionReport(ctx)
}
