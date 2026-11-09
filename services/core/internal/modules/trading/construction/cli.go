package construction

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/contracts"
)

type Session interface {
	Trading() contracts.Trading
	RequireSchema() error
	Close() error
}

type CLIResources interface {
	OpenExisting(path string) (Session, error)
	OpenReadOnly(context.Context, string) (Session, error)
	OpenReadWrite(context.Context, string) (Session, error)
}
