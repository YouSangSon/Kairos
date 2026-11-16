package portfolio

import (
	"omni-folio/services/core/internal/modules/portfolio/contracts"
	"omni-folio/services/core/internal/modules/portfolio/internal/application"
	"time"
)

func Compose(inputs contracts.InputsReader, now func() time.Time) contracts.Queries {
	return &application.Queries{Inputs: inputs, Now: now}
}
