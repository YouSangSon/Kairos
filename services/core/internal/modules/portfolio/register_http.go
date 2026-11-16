package portfolio

import (
	"net/http"
	"omni-folio/services/core/internal/modules/portfolio/contracts"
	inbound "omni-folio/services/core/internal/modules/portfolio/internal/adapters/http"
	"time"
)

func RegisterHTTP(mux *http.ServeMux, api contracts.Queries, now func() time.Time) {
	inbound.Register(mux, api, now)
}
