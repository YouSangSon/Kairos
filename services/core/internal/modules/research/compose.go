package research

import (
	"time"

	"omni-folio/services/core/internal/modules/research/contracts"
	"omni-folio/services/core/internal/modules/research/internal/adapters/files"
	"omni-folio/services/core/internal/modules/research/internal/application"
)

type Config struct {
	FeedPath func() string
	Now      func() time.Time
}

func Compose(config Config) contracts.Queries {
	return &application.Queries{Feeds: &files.Reader{Path: config.FeedPath}, Now: config.Now}
}
