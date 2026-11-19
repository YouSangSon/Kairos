package http

import (
	"net/http"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"time"
)

type handler struct {
	api c.Queries
	now func() time.Time
}

func Register(mux *http.ServeMux, api c.Queries, now func() time.Time) {
	h := &handler{api: api, now: now}
	mux.HandleFunc("GET /v1/portfolio/cash-valuation", h.handleCashValuation)
	mux.HandleFunc("GET /v1/portfolio/holding-valuation", h.handleHoldingValuation)
	mux.HandleFunc("GET /v1/portfolio/valuation-evidence", h.handleValuationEvidence)
	mux.HandleFunc("GET /v1/portfolio/current-valuation", h.handleCurrentPortfolioValuation)
	mux.HandleFunc("GET /v1/portfolio/xirr", h.handlePortfolioXIRR)
	mux.HandleFunc("GET /v1/portfolio/twr", h.handlePortfolioTWR)
	mux.HandleFunc("GET /v1/portfolio/twr/checkpoints", h.handlePortfolioTWRCheckpoints)
	mux.HandleFunc("GET /v1/portfolio/benchmarks", h.handlePortfolioBenchmarks)
}
