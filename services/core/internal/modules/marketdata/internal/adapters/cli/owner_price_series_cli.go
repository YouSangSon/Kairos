package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	privatefile "omni-folio/services/core/internal/files/private"
	codec "omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
)

func runOwnerPriceImportCommand(ctx context.Context, args []string, config Config, out io.Writer) error {
	if ctx == nil {
		return errors.New("owner price command context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("owner-price-import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	inputPath := fs.String("input", "", "bounded complete owner price series JSON")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *dbPath == "" || *inputPath == "" {
		return errors.New("owner-price-import requires only -db and -input")
	}
	raw, err := privatefile.ReadBounded(*inputPath, 1<<20)
	if err != nil {
		return errors.New("owner price input must be a bounded readable regular file")
	}
	if _, _, _, err := codec.DecodeOwnerPriceSeries(ctx, raw, config.CanonicalInstrumentID); err != nil {
		return err
	}
	db, err := config.Resources.OpenReadWrite(ctx, *dbPath)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("owner price database is unavailable")
	}
	defer db.Close()
	receipt, err := db.OwnerPrice().ImportOwnerPrice(ctx, raw)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("owner price import could not verify and store the complete version")
	}
	return json.NewEncoder(out).Encode(receipt)
}
