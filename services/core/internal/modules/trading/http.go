package trading

import (
	"net/http"
	"omni-folio/services/core/internal/modules/trading/contracts"
	adapter "omni-folio/services/core/internal/modules/trading/internal/adapters/http"
)

func RegisterHTTP(mux *http.ServeMux, service contracts.Trading) { adapter.Register(mux, service) }
