package ledger

import (
	"net/http"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	httpadapter "omni-folio/services/core/internal/modules/ledger/internal/adapters/http"
)

func RegisterHTTP(mux *http.ServeMux, api c.Service, cursorKey [32]byte) {
	httpadapter.Register(mux, api, cursorKey)
}
