package domain

import (
	"errors"
	"strings"
	"time"
)

type valuationFXInputs struct {
	observations []FXObservation
	times        map[string][3]time.Time
}
type valuationPriceInputs struct {
	observations []SecurityPriceObservation
	times        map[string][3]time.Time
	lastSplits   map[string]time.Time
}

func fxInputs(all Inputs) (valuationFXInputs, error) {
	ts, err := currentFXObservationTimes(all.Market.FX)
	return valuationFXInputs{all.Market.FX, ts}, err
}
func priceInputs(all Inputs) (valuationPriceInputs, error) {
	ts, err := currentPriceObservationTimes(all.Market.Prices)
	splits := map[string]time.Time{}
	for _, s := range all.Ledger.LastSplits {
		splits[ownerPriceKey(s.InstrumentID, s.Currency)] = s.OccurredAt
	}
	return valuationPriceInputs{all.Market.Prices, ts, splits}, err
}
func ownerPriceKey(id, currency string) string   { return id + "\x00" + currency }
func instrumentIDForSymbol(symbol string) string { return "instrument_" + strings.ToLower(symbol) }
func ValidateInputs(all Inputs) error {
	if !all.Cutoff.Equal(all.Market.Cutoff) {
		return errors.New("market inputs do not belong to the valuation observation")
	}
	return nil
}
func benchmarkReceipt(r *benchmarkSeriesRecord) *BenchmarkSeriesReceipt {
	i := r.Input
	return &BenchmarkSeriesReceipt{StorageCheck: "verified", Authority: "owner_declared", BenchmarkID: i.BenchmarkID, Label: i.Label, Currency: i.Currency, ReturnBasis: i.ReturnBasis, SourceReference: i.SourceReference, SourceVersion: i.SourceVersion, Sample: i.Sample, DataSHA256: r.DataSHA256, PointCount: len(i.Points), FirstObservedAt: i.Points[0].ObservedAt, LastObservedAt: i.Points[len(i.Points)-1].ObservedAt, FetchedAt: i.FetchedAt, RecordedAt: r.RecordedAt}
}
func fxTimeKey(fx FXObservation) string {
	if fx.Source == ownerFXSource {
		// Verified fixture identifiers cannot contain NUL; public IDs remain unchanged.
		return "\x00" + fx.ObservationID
	}
	return fx.ObservationID
}

func fxIsSample(fx *FXObservation) bool {
	return fx != nil && (fx.Source == "local_fixture" || fx.OwnerSample)
}

func supportedValuationFX(fx FXObservation) bool {
	return fx.Source == "local_fixture" || fx.Source == ownerFXSource
}

func ownerFXIssue() APIError {
	return APIError{Code: "owner_declared_fx", Message: "selected FX inputs were declared by the owner, not verified by a market provider"}
}

func usesOwnerFX(issues []APIError) bool {
	for _, issue := range issues {
		if issue.Code == "owner_declared_fx" {
			return true
		}
	}
	return false
}

func priceTimeKey(p SecurityPriceObservation) string {
	if p.Source == ownerPriceSource {
		return "\x00" + p.ObservationID
	}
	return p.ObservationID
}

func priceIsSample(p *SecurityPriceObservation) bool {
	return p != nil && (p.Source == "local_fixture" || p.OwnerSample)
}

func ownerPriceIssue() APIError {
	return APIError{Code: "owner_declared_price", Message: "selected security prices were declared by the owner, not verified by a market provider"}
}

func usesOwnerPrice(issues []APIError) bool {
	for _, i := range issues {
		if i.Code == "owner_declared_price" {
			return true
		}
	}
	return false
}

func (p valuationPriceInputs) afterLastSplit(mark *SecurityPriceObservation) bool {
	if mark.Source != ownerPriceSource {
		return true
	}
	last := p.lastSplits[ownerPriceKey(mark.InstrumentID, mark.Currency)]
	return last.IsZero() || p.times[priceTimeKey(*mark)][0].After(last)
}
