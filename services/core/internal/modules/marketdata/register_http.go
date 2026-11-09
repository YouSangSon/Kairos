package marketdata

import (
	"net/http"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	httpadapter "omni-folio/services/core/internal/modules/marketdata/internal/adapters/http"
	"time"
)

func RegisterHTTP(mux *http.ServeMux, module Module, ownerPrice c.OwnerPriceImports, candles func() c.CandleSource, canonical func(string) string, now func() time.Time) {
	httpadapter.Register(mux, module.Imports, module.Observations, ownerPrice, candles, canonical, now)
}
