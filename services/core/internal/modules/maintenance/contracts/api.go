package contracts

import "context"

type Migrations interface{ Migrate(context.Context) error }

type BackupRequest struct{ OutputPath, GoldenPath, ManifestPath string }
type Backups interface {
	CreateBackup(context.Context, BackupRequest) (*BackupManifest, error)
	VerifyRestore(context.Context, string, string) error
	VerifyManifest(context.Context, string, string, string) error
}

type Schema interface {
	RequireCurrent(context.Context) error
	RequireStartup(context.Context) error
}
