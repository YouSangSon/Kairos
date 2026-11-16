package domain

import (
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
)

func ValidateBarValues(open, high, low, close, volume string) error {
	prices := make([]*big.Rat, 4)
	for index, item := range []struct{ name, raw string }{{"open", open}, {"high", high}, {"low", low}, {"close", close}} {
		value, err := exact.ParseDecimal(item.raw)
		if err != nil || value.Sign() <= 0 {
			return fmt.Errorf("has invalid %s", item.name)
		}
		prices[index] = value
	}
	parsedVolume, err := exact.ParseDecimal(volume)
	if err != nil || parsedVolume.Sign() < 0 {
		return errors.New("has invalid volume")
	}
	if prices[2].Cmp(prices[0]) > 0 || prices[2].Cmp(prices[3]) > 0 ||
		prices[0].Cmp(prices[1]) > 0 || prices[3].Cmp(prices[1]) > 0 {
		return errors.New("has invalid OHLC range")
	}
	return nil
}
