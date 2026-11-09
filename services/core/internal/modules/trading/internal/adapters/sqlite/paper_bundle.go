package sqlite

import (
	"omni-folio/services/core/internal/modules/trading/internal/adapters/files"
)

const maxPaperBundleBytes = 4 * maxBodyBytes

var decodePaperInputBundle = files.DecodePaperInputBundle

var validJSONSurrogates = files.ValidJSONSurrogates
