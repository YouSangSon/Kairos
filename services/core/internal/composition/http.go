package composition

import (
	"net/http"
	ledger "omni-folio/services/core/internal/modules/ledger"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	market "omni-folio/services/core/internal/modules/marketdata"
	mc "omni-folio/services/core/internal/modules/marketdata/contracts"
	portfolio "omni-folio/services/core/internal/modules/portfolio"
	research "omni-folio/services/core/internal/modules/research"
	trading "omni-folio/services/core/internal/modules/trading"
	watchlist "omni-folio/services/core/internal/modules/watchlist"
	"omni-folio/services/core/internal/transport/httpjson"
)

func (r *Runtime) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	maintenance.RegisterHTTP(mux, r.Maintenance.Schema)
	ledger.RegisterHTTP(mux, r.Ledger, r.activityCursorKey)
	market.RegisterHTTP(mux, r.Market, r.OwnerPrice, func() mc.CandleSource { return r.Market.Candles }, r.Ledger.CanonicalInstrumentID, r.now)
	portfolio.RegisterHTTP(mux, r.Portfolio, r.now)
	watchlist.RegisterHTTP(mux, r.Watchlist.API)
	trading.RegisterHTTP(mux, r.Trading)
	research.RegisterHTTP(mux, r.Research)
	return mux
}
