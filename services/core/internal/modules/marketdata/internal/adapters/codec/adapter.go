package codec

import (
	"context"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
)

type Decoder struct{ canonical func(string) string }

func New(canonical func(string) string) *Decoder { return &Decoder{canonical} }
func (d *Decoder) OwnerFX(ctx context.Context, raw []byte) (*c.OwnerFXSeriesInput, []byte, string, error) {
	return DecodeOwnerFXSeries(ctx, raw)
}
func (d *Decoder) OwnerPrice(ctx context.Context, raw []byte) (*c.OwnerPriceSeriesInput, []byte, string, error) {
	return DecodeOwnerPriceSeries(ctx, raw, d.canonical)
}
func (d *Decoder) Benchmark(ctx context.Context, raw []byte) (*c.BenchmarkSeriesInput, []byte, string, error) {
	return DecodeBenchmarkSeries(ctx, raw)
}
func (d *Decoder) FX(input c.FXObservationInput) error { return ValidateFXObservationInput(input) }
func (d *Decoder) Price(input c.SecurityPriceObservationInput) error {
	return ValidateSecurityPriceObservationInput(input)
}
func (d *Decoder) Listing(input c.InstrumentListingInput) error {
	return ValidateInstrumentListingInput(input)
}

func (d *Decoder) PriceSourceID(input c.SecurityPriceObservationInput) (string, error) {
	return KiwoomLatestTradeObservationID(input.Source, input.InstrumentID, input.Symbol, input.Venue, input.Currency, input.PriceAdjustment, input.ObservedAt)
}
