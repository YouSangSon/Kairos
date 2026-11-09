package contracts

import "errors"

var ErrOwnerPriceInput = errors.New("owner price series contract is invalid")
var ErrOwnerFXInput = errors.New("owner FX series contract is invalid")
var ErrBenchmarkInput = errors.New("benchmark index series contract is invalid")
var ErrBenchmarkNotFound = errors.New("benchmark index series was not found")
var ErrRejected = errors.New("owner data input is not admissible")
var ErrConflict = errors.New("owner data input conflicts with stored identity or source")
var ErrNotFound = errors.New("market observation not found")
var ErrListingNotFound = errors.New("instrument listing not found")
var ErrFXNotFound = errors.New("FX observation not found")
var ErrMarketDataNotFound = errors.New("market data not found")
