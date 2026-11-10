package construction

import (
	"context"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
)

type Session interface {
	Ledger() c.Service
	RequireSchema() error
	Close() error
}
type CLIResources interface {
	OpenExisting(string) (Session, error)
	OpenReadOnly(context.Context, string) (Session, error)
	OpenReadWrite(context.Context, string) (Session, error)
}
