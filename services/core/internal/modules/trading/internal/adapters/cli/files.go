package cli

import (
	"bytes"
	"errors"
	"omni-folio/services/core/internal/modules/trading/internal/adapters/files"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
)

func splitPaperBundle(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		if i+1 > maxPaperBundleBytes {
			return 0, nil, errors.New("paper stream frame exceeds 4 MiB")
		}
		return i + 1, data[:i+1], nil
	}
	if len(data) >= maxPaperBundleBytes || (atEOF && len(data) > 0) {
		return 0, nil, errors.New("paper stream frame is oversized or missing LF")
	}
	return 0, nil, nil
}

const (
	maxBodyBytes        = 1 << 20
	maxMarketDataBytes  = 1 << 20
	maxPaperBundleBytes = 4 * maxBodyBytes
)

var (
	orderAlias  = domain.OrderAlias
	safeOrderID = domain.SafeOrderID
)
var readStrategyArtifact = files.ReadStrategyArtifact
var readBoundedRegularFile = files.ReadBoundedRegularFile
var decodePaperInputBundle = files.DecodePaperInputBundle
var validJSONSurrogates = files.ValidJSONSurrogates
var decodePaperProposal = files.DecodePaperProposal
