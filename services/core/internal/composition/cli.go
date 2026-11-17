package composition

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	ledger "omni-folio/services/core/internal/modules/ledger"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	market "omni-folio/services/core/internal/modules/marketdata"
	trading "omni-folio/services/core/internal/modules/trading"
	"os"
	"time"
)

type CLIOptions struct {
	MockReadRuntime func(Resources) MockReadRuntime
}
type MockReadRuntime struct {
	Ledger ledger.CLIConfig
	Market market.CLIConfig
	Close  func()
}

var mockReadRuntimeFactory = func(resources Resources) MockReadRuntime {
	transport, closeTransport := resources.MockReadTransport()
	return MockReadRuntime{
		Ledger: ledger.CLIConfig{Resources: resources.Ledger(), Transport: transport, Now: resources.Now, Pacer: resources.MockReadPacer()},
		Market: market.CLIConfig{Resources: resources.Market(), CanonicalInstrumentID: ledger.ComposeIdentifiers(), Transport: transport, Now: resources.Now, Pacer: resources.MockReadPacer()},
		Close:  closeTransport,
	}
}

func RunCLI(ctx context.Context, args []string, options CLIOptions, input *os.File, output, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: omni-core <migrate|ledger-replay-preview|ledger-replay-activate|ledger-export|benchmark-import|benchmark-status|owner-fx-import|owner-price-import|kiwoom-mock-sync|kiwoom-mock-candles|serve|backup|verify-restore|strategy-register|strategy-status|strategy-select|strategy-rollback|paper-run-due|paper-run-loop|paper-init|paper-import-bars|paper-execute|paper-execute-stream|paper-input-status|paper-improve|paper-promote|paper-promotion-status|paper-promotion-abort>")
	}
	resources := NewResources()
	factory := options.MockReadRuntime
	if factory == nil {
		factory = mockReadRuntimeFactory
	}
	switch args[0] {
	case "owner-price-import", "owner-fx-import", "benchmark-import", "benchmark-status":
		return market.RunCLI(ctx, args[0], args[1:], market.CLIConfig{Resources: resources.Market(), CanonicalInstrumentID: ledger.ComposeIdentifiers()}, output)
	case "kiwoom-mock-candles":
		runtime := factory(resources)
		defer runtime.Close()
		return market.RunCLI(ctx, args[0], args[1:], runtime.Market, output)
	case "ledger-replay-preview", "ledger-replay-activate", "ledger-export":
		return ledger.RunCLI(ctx, args[0], args[1:], ledger.CLIConfig{Resources: resources.Ledger(), Stderr: stderr}, output)
	case "kiwoom-mock-sync":
		runtime := factory(resources)
		defer runtime.Close()
		runtime.Ledger.Stderr = stderr
		return ledger.RunCLI(ctx, args[0], args[1:], runtime.Ledger, output)
	case "migrate", "backup", "verify-restore":
		return maintenance.RunCLI(ctx, args[0], args[1:], resources.Maintenance(), stderr)
	case "strategy-register", "strategy-status", "strategy-select", "strategy-rollback", "paper-run-due", "paper-run-loop", "paper-init", "paper-import-bars", "paper-execute", "paper-execute-stream", "paper-input-status", "paper-improve", "paper-promote", "paper-promotion-status", "paper-promotion-abort":
		return trading.RunCLI(ctx, args[0], args[1:], resources.Trading(), input, output)
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		addr := fs.String("addr", "127.0.0.1:8080", "listen address")
		allowOrigin := fs.String("allow-origin", "", "exact browser origin allowed for local development")
		marketFixture := fs.String("market-fixture", "", "optional local market-data CSV fixture path")
		researchFeed := fs.String("research-feed", "", "optional local Mimir public research file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := ValidateAllowedOrigin(*allowOrigin); err != nil {
			return err
		}
		runtime, err := resources.OpenServer(*dbPath, *marketFixture, *researchFeed)
		if err != nil {
			return err
		}
		defer runtime.Close()
		srv := &http.Server{
			Addr:              *addr,
			Handler:           WithCORS(runtime.Routes(), *allowOrigin),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		listener, err := net.Listen("tcp", *addr)
		if err != nil {
			return err
		}
		log.Printf("omni-core listening on %s", listener.Addr())
		return serveHTTP(ctx, srv, listener, 10*time.Second)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
