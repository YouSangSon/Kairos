package watchlist

import (
	"net/http"
	"omni-folio/services/core/internal/modules/watchlist/contracts"
	inbound "omni-folio/services/core/internal/modules/watchlist/internal/adapters/http"
)

func RegisterHTTP(mux *http.ServeMux, api contracts.API) {
	inbound.Register(mux, api)
}
