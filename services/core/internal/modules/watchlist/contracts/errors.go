package contracts

import "errors"

var ErrInput = errors.New("invalid watchlist input")
var ErrChanged = errors.New("watchlist revision changed")

const BodyLimit = 16 * 1024
const ResponseLimit = 128 * 1024
const RevisionLimit int64 = 9007199254740991
