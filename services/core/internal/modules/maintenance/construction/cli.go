package construction

import (
	"context"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
)

type Session interface {
	Migrations() c.Migrations
	Backups() c.Backups
	Close() error
}
type CLIResources interface {
	Open(string) (Session, error)
	OpenExisting(string) (Session, error)
	VerifyManifest(context.Context, string, string, string) error
}
