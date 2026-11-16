package application

import (
	"errors"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/modules/watchlist/internal/domain"
	"time"
)

func domainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInput):
		return c.ErrInput
	case errors.Is(err, domain.ErrChanged):
		return c.ErrChanged
	default:
		return err
	}
}
func domainEntries(entries []c.WatchlistEntry) []domain.WatchlistEntry {
	if entries == nil {
		return nil
	}
	result := make([]domain.WatchlistEntry, len(entries))
	for i, e := range entries {
		result[i] = domain.WatchlistEntry(e)
	}
	return result
}
func publicEntries(entries []domain.WatchlistEntry) []c.WatchlistEntry {
	if entries == nil {
		return nil
	}
	result := make([]c.WatchlistEntry, len(entries))
	for i, e := range entries {
		result[i] = c.WatchlistEntry(e)
	}
	return result
}
func domainState(state c.WatchlistState) domain.WatchlistState {
	return domain.WatchlistState{Revision: state.Revision, UpdatedAt: state.UpdatedAt, Entries: domainEntries(state.Entries)}
}
func publicState(state domain.WatchlistState) c.WatchlistState {
	return c.WatchlistState{Revision: state.Revision, UpdatedAt: state.UpdatedAt, Entries: publicEntries(state.Entries)}
}
func CanonicalEntries(entries []c.WatchlistEntry) ([]c.WatchlistEntry, error) {
	canonical, err := domain.CanonicalEntries(domainEntries(entries))
	return publicEntries(canonical), domainError(err)
}
func ValidateChange(change c.WatchlistChange) error {
	_, err := domain.ValidateChange(domain.WatchlistChange{ExpectedRevision: change.ExpectedRevision, Entries: domainEntries(change.Entries)})
	return domainError(err)
}
func StateMetadataValid(state c.WatchlistState) bool {
	return domain.StateMetadataValid(domainState(state))
}
func Transition(state *c.WatchlistState, change c.WatchlistChange, now func() time.Time) (*c.WatchlistState, bool, error) {
	canonical := domainState(*state)
	next, changed, err := domain.Transition(&canonical, domain.WatchlistChange{ExpectedRevision: change.ExpectedRevision, Entries: domainEntries(change.Entries)}, now)
	if err != nil {
		return nil, false, domainError(err)
	}
	result := publicState(*next)
	return &result, changed, nil
}
func domainInputs(inputs c.Inputs) domain.Inputs {
	result := domain.Inputs{Cutoff: inputs.Cutoff, State: domainState(inputs.State)}
	if inputs.Prices != nil {
		result.Prices = make([]domain.SecurityPriceObservation, len(inputs.Prices))
		for i, p := range inputs.Prices {
			result.Prices[i] = domain.SecurityPriceObservation(p)
		}
	}
	return result
}
func publicReport(report *domain.WatchlistReport) *c.WatchlistReport {
	if report == nil {
		return nil
	}
	result := &c.WatchlistReport{PolicyVersion: report.PolicyVersion, MaxObservationAgeSeconds: report.MaxObservationAgeSeconds, ObservedAt: report.ObservedAt, Watchlist: publicState(report.Watchlist)}
	if report.Quotes != nil {
		result.Quotes = make([]c.WatchlistQuote, len(report.Quotes))
		for i, q := range report.Quotes {
			quote := c.WatchlistQuote{WatchlistEntry: c.WatchlistEntry(q.WatchlistEntry), Status: q.Status}
			if q.Price != nil {
				price := c.WatchlistPrice(*q.Price)
				quote.Price = &price
			}
			result.Quotes[i] = quote
		}
	}
	return result
}
