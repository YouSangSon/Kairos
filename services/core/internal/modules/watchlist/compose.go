package watchlist

import (
	"database/sql"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/modules/watchlist/internal/adapters/jsoninput"
	"omni-folio/services/core/internal/modules/watchlist/internal/adapters/sqlite"
	"omni-folio/services/core/internal/modules/watchlist/internal/application"
	"omni-folio/services/core/internal/storage/observation"
	"time"
)

type Module struct {
	API      c.API
	States   c.StateReader
	Recovery c.Recovery
}

func Compose(db *sql.DB, registry *observation.Registry, inputs c.InputsReader, now func() time.Time) (Module, error) {
	store, err := sqlite.New(db, registry, now)
	if err != nil {
		return Module{}, err
	}
	return Module{API: &application.Service{Inputs: inputs, Store: store}, States: store, Recovery: store}, nil
}
func DecodeChange(raw []byte) (*c.WatchlistChange, error) { return jsoninput.Decode(raw) }
