package research

import (
	"net/http"
	"omni-folio/services/core/internal/modules/research/contracts"
	transport "omni-folio/services/core/internal/modules/research/internal/adapters/http"
)

func RegisterHTTP(mux *http.ServeMux, queries contracts.Queries) { transport.Register(mux, queries) }
