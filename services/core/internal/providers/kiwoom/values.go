package kiwoom

import (
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/exact"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
)

const maxBodyBytes = 1 << 20
const marketDataAdjustmentUnspecified = "unspecified"

var canonicalUTCString = codec.CanonicalUTC
var parseDecimal = exact.ParseDecimal
var formatDecimal = exact.FormatDecimal
var errMarketDataNotFound = market.ErrMarketDataNotFound

type MarketDataSeries = market.MarketDataSeries
type MarketDataBar = market.MarketDataBar

func positiveCanonicalDecimal(raw string) bool {
	return codec.CanonicalDecimal(raw, true) && raw != "0"
}
func validateMarketDataValues(open, high, low, close, volume string) error {
	prices := make([]*big.Rat, 4)
	for index, item := range []struct{ name, raw string }{{"open", open}, {"high", high}, {"low", low}, {"close", close}} {
		value, err := parseDecimal(item.raw)
		if err != nil || value.Sign() <= 0 {
			return fmt.Errorf("has invalid %s", item.name)
		}
		prices[index] = value
	}
	parsedVolume, err := parseDecimal(volume)
	if err != nil || parsedVolume.Sign() < 0 {
		return errors.New("has invalid volume")
	}
	if prices[2].Cmp(prices[0]) > 0 || prices[2].Cmp(prices[3]) > 0 ||
		prices[0].Cmp(prices[1]) > 0 || prices[3].Cmp(prices[1]) > 0 {
		return errors.New("has invalid OHLC range")
	}
	return nil
}

const maxMarketDataRows = 500
const marketDataAdjustmentProviderAdjusted = "provider_adjusted"
