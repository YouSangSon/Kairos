package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"omni-folio/services/core/internal/modules/maintenance/construction"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
)

func Run(ctx context.Context, command string, args []string, resources construction.CLIResources, stderr io.Writer) error {
	args = append([]string{command}, args...)
	switch command {
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		db, err := resources.Open(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Migrations().Migrate(ctx)
	case "backup":
		fs := flag.NewFlagSet("backup", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dbPath := fs.String("db", "omni-folio.db", "source SQLite database path")
		out := fs.String("out", "", "backup candidate path")
		golden := fs.String("golden", "", "expected portfolio snapshot JSON")
		manifest := fs.String("manifest", "", "verified backup manifest output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" || *golden == "" || *manifest == "" {
			return fmt.Errorf("backup requires -out, -golden, and -manifest")
		}
		db, err := resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = db.Backups().CreateBackup(ctx, c.BackupRequest{OutputPath: *out, GoldenPath: *golden, ManifestPath: *manifest})
		return err
	case "verify-restore":
		fs := flag.NewFlagSet("verify-restore", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dbPath := fs.String("db", "", "restored SQLite database path")
		golden := fs.String("golden", "", "expected portfolio snapshot JSON")
		manifest := fs.String("manifest", "", "backup manifest path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *dbPath == "" || *golden == "" || *manifest == "" {
			return fmt.Errorf("verify-restore requires -db, -golden, and -manifest")
		}
		return resources.VerifyManifest(ctx, *dbPath, *golden, *manifest)
	default:
		return errors.New("maintenance command is invalid")
	}
}
