package ledger

import (
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
)

func normalize(input Transaction) (*Transaction, []APIError) {
	result, failures := ledgerdomain.Normalize(ledgerdomain.Transaction(input))
	output := Transaction(*result)
	var issues []APIError
	for _, issue := range failures {
		issues = append(issues, APIError(issue))
	}
	return &output, issues
}
func snapshotDTO(input *ledgerdomain.PortfolioSnapshot) *contracts.PortfolioSnapshot {
	result := &contracts.PortfolioSnapshot{PortfolioID: input.PortfolioID, LedgerRevision: input.LedgerRevision, CostBasisPolicy: input.CostBasisPolicy, AsOf: input.AsOf, RecordedAt: input.RecordedAt, LiveEnabled: input.LiveEnabled, ValuationStatus: input.ValuationStatus, Cash: []contracts.Money{}, Holdings: []contracts.Holding{}, RealizedPnL: []contracts.Money{}, Provenance: contracts.Provenance{EventIDs: input.Provenance.EventIDs, ReceiptIDs: input.Provenance.ReceiptIDs}}
	for _, v := range input.Cash {
		result.Cash = append(result.Cash, contracts.Money(v))
	}
	for _, v := range input.Holdings {
		result.Holdings = append(result.Holdings, contracts.Holding(v))
	}
	for _, v := range input.RealizedPnL {
		result.RealizedPnL = append(result.RealizedPnL, contracts.Money(v))
	}
	return result
}

func (s *UseCases) CanonicalInstrumentID(symbol string) string {
	return ledgerdomain.InstrumentIDForSymbol(symbol)
}
