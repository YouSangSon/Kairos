package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	record "omni-folio/services/core/internal/codec"
	privatefile "omni-folio/services/core/internal/files/private"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	codec "omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
)

func runBenchmarkSeriesCommand(ctx context.Context, command string, args []string, config Config, out io.Writer) error {
	if ctx == nil {
		return errors.New("benchmark command context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	inputPath := fs.String("input", "", "bounded normalized index series JSON")
	id := fs.String("benchmark-id", "", "exact benchmark identity")
	version := fs.String("source-version", "", "optional exact stored version")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *dbPath == "" {
		return errors.New("benchmark command arguments are invalid")
	}
	var receipt *c.BenchmarkSeriesReceipt
	if command == "benchmark-import" {
		if *inputPath == "" || *id != "" || *version != "" {
			return errors.New("benchmark-import requires only -db and -input")
		}
		raw, err := privatefile.ReadBounded(*inputPath, 1<<20)
		if err != nil {
			return errors.New("benchmark input must be a bounded readable regular file")
		}
		// Reject the source before opening a writable database.
		if _, _, _, err := codec.DecodeBenchmarkSeries(ctx, raw); err != nil {
			return err
		}
		db, err := config.Resources.OpenReadWrite(ctx, *dbPath)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("benchmark database is unavailable")
		}
		defer db.Close()
		receipt, err = db.Imports().ImportBenchmark(ctx, raw)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("benchmark import could not verify and store the index version")
		}
	} else if command == "benchmark-status" {
		if *inputPath != "" || !record.SafeID(*id) || (*version != "" && !record.SafeID(*version)) {
			return errors.New("benchmark-status requires -db, -benchmark-id and an optional -source-version")
		}
		db, err := config.Resources.OpenReadOnly(ctx, *dbPath)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("benchmark database is unavailable")
		}
		defer db.Close()
		receipt, err = db.Imports().BenchmarkStatus(ctx, *id, *version)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("benchmark status could not verify the index version")
		}
	} else {
		return errors.New("benchmark command is invalid")
	}
	return json.NewEncoder(out).Encode(receipt)
}
