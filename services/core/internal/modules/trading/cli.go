package trading

import (
	"context"
	"io"
	"omni-folio/services/core/internal/modules/trading/construction"
	adapter "omni-folio/services/core/internal/modules/trading/internal/adapters/cli"
	"os"
)

func RunCLI(ctx context.Context, command string, args []string, resources construction.CLIResources, input *os.File, output io.Writer) error {
	return adapter.Run(ctx, command, args, resources, input, output)
}
