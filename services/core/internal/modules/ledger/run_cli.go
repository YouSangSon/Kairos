package ledger

import (
	"context"
	"io"
	"net/http"
	"omni-folio/services/core/internal/modules/ledger/construction"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	cli "omni-folio/services/core/internal/modules/ledger/internal/adapters/cli"
	"omni-folio/services/core/internal/modules/ledger/internal/adapters/files"
	"omni-folio/services/core/internal/modules/ledger/internal/adapters/provider"
	app "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/internal/providers/kiwoom"
	"time"
)

type CLIConfig struct {
	Resources  construction.CLIResources
	ReadSecret func(context.Context) ([]byte, error)
	Transport  http.RoundTripper
	Now        func() time.Time
	Pacer      *kiwoom.MockReadPacer
	Stderr     io.Writer
}

func RunCLI(ctx context.Context, command string, args []string, config CLIConfig, out io.Writer) error {
	factory := provider.MockSnapshots{ReadSecret: config.ReadSecret, Transport: config.Transport, Now: config.Now, Pacer: config.Pacer}
	return cli.Run(ctx, command, args, config.Resources, out, config.Stderr, func(broker c.Broker) app.MockSync { return app.MockSync{Broker: broker, Factory: factory} }, files.ExportPublisher{})
}
