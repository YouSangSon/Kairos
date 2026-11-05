package portfoliodomain

import "math/big"

// Check the unreduced product before allocating it, then preserve exact ratios.
func TWRChain(factor, value, capital *big.Rat) *big.Rat {
	if factor.Sign() < 0 || value.Sign() < 0 || capital.Sign() <= 0 {
		return nil
	}
	const maxBits = 8192
	if factor.Num().BitLen()+value.Num().BitLen()+capital.Denom().BitLen() > maxBits ||
		factor.Denom().BitLen()+value.Denom().BitLen()+capital.Num().BitLen() > maxBits {
		return nil
	}
	return new(big.Rat).Mul(factor, new(big.Rat).Quo(value, capital))
}
