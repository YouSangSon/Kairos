package provider

import (
	"context"
	"errors"
	app "omni-folio/services/core/internal/modules/marketdata/internal/application"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
)

type TradeSource struct{ Client *sdk.Client }

func (s TradeSource) LatestTrade(ctx context.Context, symbol string) (app.LatestTrade, error) {
	if s.Client == nil {
		return app.LatestTrade{}, errors.New("Kiwoom latest trade capture identity is invalid")
	}
	trade, err := s.Client.LatestTrade(ctx, symbol)
	if err != nil {
		return app.LatestTrade{}, err
	}
	return tradeValue(*trade), nil
}
func tradeValue(trade sdk.KiwoomLatestTrade) app.LatestTrade {
	return app.LatestTrade{Source: trade.Source, Environment: string(trade.Environment), Exchange: string(trade.Exchange), Symbol: trade.Symbol, Currency: trade.Currency, Price: trade.Price, ObservedAt: trade.ObservedAt, FetchedAt: trade.FetchedAt}
}
