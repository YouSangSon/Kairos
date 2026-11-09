package domain

import (
	"errors"
	"math/big"
)

type Projection struct {
	Quantity *big.Int `json:"-"`
	Active   bool     `json:"-"`
}

type TargetChange struct {
	Side     string
	Quantity string
}

func ChangeForTarget(projection Projection, raw string) (*TargetChange, error) {
	target, ok := new(big.Int).SetString(raw, 10)
	if !ok || target.Sign() < 0 {
		return nil, errors.New("paper target quantity is invalid")
	}
	if projection.Active {
		if projection.Quantity.Cmp(target) == 0 {
			return nil, nil
		}
		return nil, errors.New("active paper order target cannot be replaced")
	}
	delta := new(big.Int).Sub(target, projection.Quantity)
	if delta.Sign() == 0 {
		return nil, nil
	}
	side := "BUY"
	quantity := new(big.Int).Set(delta)
	if delta.Sign() < 0 {
		side = "SELL"
		quantity.Neg(quantity)
		if quantity.Cmp(projection.Quantity) > 0 {
			return nil, errors.New("paper SELL exceeds filled holdings")
		}
	}
	return &TargetChange{Side: side, Quantity: quantity.String()}, nil
}
