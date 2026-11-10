package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"omni-folio/services/core/internal/modules/ledger/construction"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	app "omni-folio/services/core/internal/modules/ledger/internal/application"
	"time"
)

func Run(ctx context.Context, command string, args []string, resources construction.CLIResources, out, stderr io.Writer, syncFactory func(c.Broker) app.MockSync, publisher app.ExportPublisher) error {
	switch command {
	case "kiwoom-mock-sync":
		return sync(ctx, args, resources, out, syncFactory)
	case "ledger-export":
		return export(ctx, args, resources, out, publisher)
	case "ledger-replay-preview", "ledger-replay-activate":
		return replay(ctx, command, args, resources, out, stderr)
	default:
		return fmt.Errorf("unknown ledger command: %s", command)
	}
}
func sync(ctx context.Context, args []string, resources construction.CLIResources, out io.Writer, create func(c.Broker) app.MockSync) error {
	if ctx == nil {
		return errors.New("kiwoom mock sync context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("kiwoom-mock-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	account := fs.String("ledger-account", "", "account-main")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *dbPath == "" || *account != "account-main" {
		return errors.New("kiwoom-mock-sync requires only -db and -ledger-account account-main")
	}
	session, err := resources.OpenReadWrite(ctx, *dbPath)
	if err != nil {
		return app.MockSyncFailure(ctx, "database is unavailable")
	}
	defer session.Close()
	receipt, err := create(session.Ledger()).Run(ctx, *account)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(receipt)
}
func export(ctx context.Context, args []string, resources construction.CLIResources, out io.Writer, publisher app.ExportPublisher) error {
	if ctx == nil {
		return errors.New("ledger export context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("ledger-export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	target := fs.String("out", "", "new private ledger activity CSV")
	if err := fs.Parse(args); err != nil {
		return errors.New("ledger-export options are invalid")
	}
	if fs.NArg() != 0 || *dbPath == "" || *target == "" {
		return errors.New("ledger-export requires -db and -out")
	}
	session, err := resources.OpenReadOnly(ctx, *dbPath)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return app.ErrExportFailed
	}
	defer session.Close()
	service := app.Export{Queries: session.Ledger(), Publisher: publisher}
	receipt, err := service.Run(ctx, *dbPath, *target)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(receipt); err != nil {
		return app.ErrExportFailed
	}
	return nil
}
func replay(ctx context.Context, command string, args []string, resources construction.CLIResources, out, stderr io.Writer) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "stopped core database or owned candidate path")
	expectedRevision := fs.Int64("expected-revision", 0, "reviewed preview ledger revision")
	expectedEvents := fs.String("expected-events-sha", "", "reviewed preview events SHA256")
	expectedSnapshot := fs.String("expected-snapshot-sha", "", "reviewed corrected snapshot SHA256")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || fs.NArg() != 0 {
		return errors.New("ledger replay command requires -db and no positional arguments")
	}
	if command == "ledger-replay-preview" && (*expectedRevision != 0 || *expectedEvents != "" || *expectedSnapshot != "") {
		return errors.New("preview accepts no activation expectations")
	}
	session, err := resources.OpenExisting(*dbPath)
	if err != nil {
		return err
	}
	defer session.Close()
	var result *c.ReplayPreview
	if command == "ledger-replay-preview" {
		result, err = session.Ledger().PreviewReplay(ctx)
	} else {
		result, err = session.Ledger().ActivateReplay(ctx, *expectedRevision, *expectedEvents, *expectedSnapshot)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
