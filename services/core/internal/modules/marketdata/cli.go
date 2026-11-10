package marketdata

import (
	"context"
	"io"
	"net/http"
	"omni-folio/services/core/internal/modules/marketdata/construction"
	adapter "omni-folio/services/core/internal/modules/marketdata/internal/adapters/cli"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"time"
)

type CLIConfig struct {
	Resources             construction.CLIResources
	CanonicalInstrumentID func(string) string
	ReadSecret            func(context.Context) ([]byte, error)
	Transport             http.RoundTripper
	Now                   func() time.Time
	Pacer                 *sdk.MockReadPacer
}

func RunCLI(ctx context.Context, command string, args []string, config CLIConfig, out io.Writer) error {
	return adapter.Run(ctx, command, args, adapter.Config{Resources: config.Resources, CanonicalInstrumentID: config.CanonicalInstrumentID, ReadSecret: config.ReadSecret, Transport: config.Transport, Now: config.Now, Pacer: config.Pacer}, out)
}
