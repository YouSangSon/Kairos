package construction

import (
	"context"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
)

type Session interface {
	Imports() c.Imports
	OwnerPrice() c.OwnerPriceImports
	Close() error
}
type CLIResources interface {
	OpenReadOnly(context.Context, string) (Session, error)
	OpenReadWrite(context.Context, string) (Session, error)
}
