package http

import (
	"context"
	"log"
	"net/http"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/transport/httpjson"
)

type reader interface {
	LocalOrderLog(context.Context) (*contracts.LocalOrderLog, error)
	PaperMonitor(context.Context) (*contracts.PaperMonitor, error)
	PaperPromotionReport(context.Context) (*contracts.PaperPromotionReportView, error)
	ResearchReport(context.Context) (*contracts.ResearchReportView, error)
}
type handler struct{ service reader }
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeInternal(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	httpjson.Write(w, http.StatusInternalServerError, apiError{Code: "internal_error", Message: "internal server error"})
}
func Register(mux *http.ServeMux, service reader) {
	h := handler{service: service}
	mux.HandleFunc("GET /v1/orders", h.orders)
	mux.HandleFunc("GET /v1/paper/monitor", h.monitor)
	mux.HandleFunc("GET /v1/paper/promotion-report", h.promotionReport)
	mux.HandleFunc("GET /v1/research/report", h.researchReport)
}

func (h handler) orders(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.LocalOrderLog(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, result)
}

func (h handler) monitor(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		httpjson.Write(w, http.StatusBadRequest, apiError{Code: "invalid_query", Message: "paper monitor accepts no query parameters"})
		return
	}
	result, err := h.service.PaperMonitor(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, http.StatusOK, result)
}

func (h handler) promotionReport(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		httpjson.Write(w, http.StatusBadRequest, apiError{Code: "invalid_query", Message: "paper promotion report accepts no query parameters"})
		return
	}
	report, err := h.service.PaperPromotionReport(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, report)
}

func (h handler) researchReport(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		httpjson.Write(w, http.StatusBadRequest, apiError{Code: "invalid_query", Message: "research report accepts no query parameters"})
		return
	}
	result, err := h.service.ResearchReport(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, http.StatusOK, result)
}
