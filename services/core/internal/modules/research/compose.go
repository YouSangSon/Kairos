package research

import (
	"net/http"
	"time"

	"omni-folio/services/core/internal/modules/research/contracts"
	"omni-folio/services/core/internal/modules/research/internal/adapters/files"
	transport "omni-folio/services/core/internal/modules/research/internal/adapters/http"
	"omni-folio/services/core/internal/modules/research/internal/application"
)

type Config struct {
	FeedPath func() string
	Now      func() time.Time
}

func Compose(config Config) contracts.Queries {
	return &application.Queries{Feeds: &files.Reader{Path: config.FeedPath}, Now: config.Now}
}
func RegisterHTTP(mux *http.ServeMux, queries contracts.Queries) { transport.Register(mux, queries) }
