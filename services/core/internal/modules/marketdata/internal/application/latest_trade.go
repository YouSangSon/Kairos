package application

import (
	"context"
	"errors"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
)

type LatestTrade struct{ Source, Environment, Exchange, Symbol, Currency, Price, ObservedAt, FetchedAt string }
type LatestTradeSource interface {
	LatestTrade(context.Context, string) (LatestTrade, error)
}

func (s *Service) CaptureLatestTrade(ctx context.Context, source LatestTradeSource, symbol string) (*c.SecurityPriceObservation, error) {
	if s == nil || source == nil || !domain.KRXSymbol(symbol) {
		return nil, errors.New("Kiwoom latest trade capture identity is invalid")
	}
	if _, err := s.ResolveListing(ctx, "XKRX", symbol, "KRW"); err != nil {
		return nil, err
	}
	trade, err := source.LatestTrade(ctx, symbol)
	if err != nil {
		return nil, err
	}
	return s.RecordLatestTrade(ctx, trade)
}
func (s *Service) RecordLatestTrade(ctx context.Context, trade LatestTrade) (*c.SecurityPriceObservation, error) {
	if s == nil {
		return nil, errors.New("Kiwoom latest trade service is unavailable")
	}
	if trade.Source != "kiwoom" || (trade.Environment != "mock" && trade.Environment != "production") || trade.Exchange != "KRX" || trade.Currency != "KRW" || !domain.KRXSymbol(trade.Symbol) {
		return nil, errors.New("Kiwoom latest trade identity is invalid")
	}
	listing, err := s.ResolveListing(ctx, "XKRX", trade.Symbol, trade.Currency)
	if err != nil {
		return nil, err
	}
	input := c.SecurityPriceObservationInput{Source: "kiwoom_" + trade.Environment, InstrumentID: listing.InstrumentID, Symbol: trade.Symbol, Venue: "XKRX", Currency: trade.Currency, Price: trade.Price, PriceAdjustment: "unspecified", ObservedAt: trade.ObservedAt, FetchedAt: trade.FetchedAt}
	input.SourceObservationID, err = s.decode.PriceSourceID(input)
	if err != nil {
		return nil, err
	}
	return s.RecordPrice(ctx, input)
}
