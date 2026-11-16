package http

import (
	"log"
	"net/http"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/transport/httpjson"
)

func Register(mux *http.ServeMux, schema c.Schema) {
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := schema.RequireCurrent(r.Context()); err != nil {
			log.Printf("readiness check failed: %v", err)
			httpjson.Write(w, http.StatusServiceUnavailable, struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{"not_ready", "service is not ready"})
			return
		}
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}
