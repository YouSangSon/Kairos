package domain

import (
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"time"
)

type PaperMarketObservation struct {
	Source            string `json:"source"`
	Symbol            string `json:"symbol"`
	ObservedAt        string `json:"observed_at"`
	AskPrice          string `json:"ask_price"`
	AvailableQuantity string `json:"available_quantity"`
}

func ValidatePaperSignal(signal PaperSignal) (*big.Int, time.Time, time.Time, error) {
	dataAsOf, dataOK := CanonicalUTCTime(signal.DataAsOf)
	generatedAt, generatedOK := CanonicalUTCTime(signal.GeneratedAt)
	expiresAt, expiresOK := CanonicalUTCTime(signal.ExpiresAt)
	if len(signal.TargetQuantity) == 0 || len(signal.TargetQuantity) > 64 {
		return nil, time.Time{}, time.Time{}, errors.New("paper signal is invalid")
	}
	target, targetOK := new(big.Int).SetString(signal.TargetQuantity, 10)
	if signal.SchemaVersion != paperSignalSchema || !safeOrderID(signal.SignalID) || !kiwoomStockPattern.MatchString(signal.Symbol) ||
		!strategySHA256Pattern.MatchString(signal.StrategyResultSHA256) || !safeOrderID(signal.StrategySelectionEventID) ||
		!strategySHA256Pattern.MatchString(signal.DataSHA256) || !targetOK || target.Sign() <= 0 || !validOrderInteger(signal.TargetQuantity) ||
		!dataOK || !generatedOK || !expiresOK || dataAsOf.After(generatedAt) || !generatedAt.Before(expiresAt) ||
		!safeOrderID("paper_"+signal.SignalID) {
		return nil, time.Time{}, time.Time{}, errors.New("paper signal is invalid")
	}
	return target, generatedAt, expiresAt, nil
}
func ValidatePaperObservation(observation PaperMarketObservation, signal PaperSignal, generatedAt, now time.Time) (time.Time, *big.Rat, *big.Rat, error) {
	observedAt, timeOK := CanonicalUTCTime(observation.ObservedAt)
	ask, askErr := exact.ParseDecimal(observation.AskPrice)
	available, quantityErr := exact.ParseDecimal(observation.AvailableQuantity)
	if observation.Source != "local_fixture" || observation.Symbol != signal.Symbol || !timeOK ||
		!observedAt.After(generatedAt) || observedAt.After(now) || askErr != nil || ask.Sign() <= 0 ||
		quantityErr != nil || available.Sign() <= 0 || !validOrderInteger(observation.AvailableQuantity) {
		return time.Time{}, nil, nil, errors.New("paper market observation is invalid")
	}
	return observedAt, ask, available, nil
}
