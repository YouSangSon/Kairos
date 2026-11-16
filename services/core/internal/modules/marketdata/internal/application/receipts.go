package application

import c "omni-folio/services/core/internal/modules/marketdata/contracts"

func stringPointer(s string) *string { return &s }
func OwnerFXReceipt(r *OwnerFXRecord) *c.OwnerFXSeriesReceipt {
	i := r.Input
	receipt := &c.OwnerFXSeriesReceipt{StorageCheck: "verified", Authority: "owner_declared", SourceVersion: i.SourceVersion, BaseCurrency: i.BaseCurrency, QuoteCurrency: i.QuoteCurrency, Sample: i.Sample, DataSHA256: r.DataSHA256, PointCount: len(i.Points), FetchedAt: i.FetchedAt, RecordedAt: r.RecordedAt}
	if len(i.Points) > 0 {
		receipt.FirstObservedAt = stringPointer(i.Points[0].ObservedAt)
		receipt.LastObservedAt = stringPointer(i.Points[len(i.Points)-1].ObservedAt)
	}
	return receipt
}
func OwnerPriceReceipt(r *OwnerPriceRecord) *c.OwnerPriceSeriesReceipt {
	i := r.Input
	receipt := &c.OwnerPriceSeriesReceipt{StorageCheck: "verified", Authority: "owner_declared", SourceVersion: i.SourceVersion, InstrumentID: i.InstrumentID, Symbol: i.Symbol, Venue: i.Venue, PriceAdjustment: i.PriceAdjustment, Currency: i.Currency, Sample: i.Sample, DataSHA256: r.DataSHA256, PointCount: len(i.Points), FetchedAt: i.FetchedAt, RecordedAt: r.RecordedAt}
	if len(i.Points) > 0 {
		receipt.FirstObservedAt = stringPointer(i.Points[0].ObservedAt)
		receipt.LastObservedAt = stringPointer(i.Points[len(i.Points)-1].ObservedAt)
	}
	return receipt
}
func BenchmarkReceipt(r *BenchmarkRecord) *c.BenchmarkSeriesReceipt {
	i := r.Input
	return &c.BenchmarkSeriesReceipt{StorageCheck: "verified", Authority: "owner_declared", BenchmarkID: i.BenchmarkID, Label: i.Label, Currency: i.Currency, ReturnBasis: i.ReturnBasis, SourceReference: i.SourceReference, SourceVersion: i.SourceVersion, Sample: i.Sample, DataSHA256: r.DataSHA256, PointCount: len(i.Points), FirstObservedAt: i.Points[0].ObservedAt, LastObservedAt: i.Points[len(i.Points)-1].ObservedAt, FetchedAt: i.FetchedAt, RecordedAt: r.RecordedAt}
}
