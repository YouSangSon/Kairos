package maintenance

import (
	"context"
	"io"
	"omni-folio/services/core/internal/modules/maintenance/construction"
	adapter "omni-folio/services/core/internal/modules/maintenance/internal/adapters/cli"
)

func RunCLI(ctx context.Context, command string, args []string, resources construction.CLIResources, stderr io.Writer) error {
	return adapter.Run(ctx, command, args, resources, stderr)
}
