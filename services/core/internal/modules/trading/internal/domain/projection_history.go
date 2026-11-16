package domain

import (
	"errors"
	"math/big"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
)

func AccumulatePaperProjection(quantity *big.Int, activeCount int, intent OrderIntent, state *orderdomain.State) (int, error) {
	total, totalOK := new(big.Int).SetString(state.Quantity, 10)
	filled, filledOK := new(big.Int).SetString(state.FilledQuantity, 10)
	if !totalOK || !filledOK || filled.Sign() < 0 || filled.Cmp(total) > 0 {
		return activeCount, errors.New("paper projected quantity is invalid")
	}
	sign := int64(1)
	if intent.Side == "SELL" {
		sign = -1
	}
	quantity.Add(quantity, new(big.Int).Mul(filled, big.NewInt(sign)))
	active := state.Status == "RECORDED" || state.Status == "READY" || ReservationIsActive(state)
	if active {
		activeCount++
		remaining := new(big.Int).Sub(total, filled)
		quantity.Add(quantity, remaining.Mul(remaining, big.NewInt(sign)))
	}
	return activeCount, nil
}
func ValidatePaperProjection(projected *big.Int, activeCount int) error {
	if activeCount > 1 || projected.Sign() < 0 {
		return errors.New("paper projection violates the one-active-order or long-only boundary")
	}
	return nil
}

type PaperIntentDeltaHistory struct {
	positions map[string]*big.Int
	active    map[string]string
	filled    map[string]*big.Int
}

func NewPaperIntentDeltaHistory() *PaperIntentDeltaHistory {
	return &PaperIntentDeltaHistory{positions: map[string]*big.Int{}, active: map[string]string{}, filled: map[string]*big.Int{}}
}
func (h *PaperIntentDeltaHistory) RecordIntent(orderID string, intent OrderIntent) error {
	key := intent.AccountRef + "\x00" + intent.Symbol
	if h.positions[key] == nil {
		h.positions[key] = new(big.Int)
	}
	if h.active[key] != "" {
		return errors.New("capitalized paper intents cross an active order")
	}
	target, _ := new(big.Int).SetString(intent.SignalTargetQuantity, 10)
	delta := new(big.Int).Sub(target, h.positions[key])
	quantity, _ := new(big.Int).SetString(intent.Quantity, 10)
	if delta.Sign() == 0 || new(big.Int).Abs(delta).Cmp(quantity) != 0 || (delta.Sign() > 0) != (intent.Side == "BUY") {
		return errors.New("capitalized paper intent target delta is invalid")
	}
	h.active[key], h.filled[orderID] = orderID, new(big.Int)
	return nil
}
func (h *PaperIntentDeltaHistory) RecordFill(orderID string, intent OrderIntent, rawQuantity string) error {
	key := intent.AccountRef + "\x00" + intent.Symbol

	quantity, _ := new(big.Int).SetString(rawQuantity, 10)
	if intent.Side == "BUY" {
		h.positions[key].Add(h.positions[key], quantity)
	} else {
		h.positions[key].Sub(h.positions[key], quantity)
		if h.positions[key].Sign() < 0 {
			return errors.New("capitalized paper SELL oversold holdings")
		}
	}
	h.filled[orderID].Add(h.filled[orderID], quantity)
	total, _ := new(big.Int).SetString(intent.Quantity, 10)
	if h.filled[orderID].Cmp(total) > 0 {
		return errors.New("capitalized paper fill exceeds its order")
	}
	if h.filled[orderID].Cmp(total) == 0 {
		delete(h.active, key)
	}
	return nil
}
