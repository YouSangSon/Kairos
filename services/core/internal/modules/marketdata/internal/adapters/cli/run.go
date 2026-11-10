package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"omni-folio/services/core/internal/modules/marketdata/construction"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"time"
)

type Config struct {
	Resources             construction.CLIResources
	CanonicalInstrumentID func(string) string
	ReadSecret            func(context.Context) ([]byte, error)
	Transport             http.RoundTripper
	Now                   func() time.Time
	Pacer                 *sdk.MockReadPacer
}

func Run(ctx context.Context, command string, args []string, config Config, out io.Writer) error {
	switch command {
	case "owner-price-import":
		return runOwnerPriceImportCommand(ctx, args, config, out)
	case "owner-fx-import":
		return runOwnerFXImportCommand(ctx, args, config, out)
	case "benchmark-import", "benchmark-status":
		return runBenchmarkSeriesCommand(ctx, command, args, config, out)
	case "kiwoom-mock-candles":
		return runCandles(ctx, args, out, config)
	default:
		return errors.New("market command is invalid")
	}
}
