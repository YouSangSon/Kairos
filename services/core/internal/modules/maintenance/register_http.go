package maintenance

import (
	"net/http"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
	adapter "omni-folio/services/core/internal/modules/maintenance/internal/adapters/http"
)

func RegisterHTTP(mux *http.ServeMux, schema c.Schema) { adapter.Register(mux, schema) }
