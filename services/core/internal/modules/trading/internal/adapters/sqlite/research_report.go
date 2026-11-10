package sqlite

import (
	"context"

	"omni-folio/services/core/internal/modules/trading/contracts"
)

type ResearchReportView = contracts.ResearchReportView
type StoredResearchReport = contracts.StoredResearchReport
type ResearchReportRange = contracts.ResearchReportRange
type ResearchReportMetrics = contracts.ResearchReportMetrics

func (s *Store) ResearchReport(ctx context.Context) (*ResearchReportView, error) {
	return s.business().ResearchReport(ctx)
}
