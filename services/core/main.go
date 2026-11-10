package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"omni-folio/services/core/internal/composition"
	ledger "omni-folio/services/core/internal/modules/ledger"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	market "omni-folio/services/core/internal/modules/marketdata"
	trading "omni-folio/services/core/internal/modules/trading"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type mockReadRuntime struct {
	ledger ledger.CLIConfig
	market market.CLIConfig
	close  func()
}

var mockReadRuntimeFactory = func(resources composition.Resources) mockReadRuntime {
	transport, closeTransport := resources.MockReadTransport()
	return mockReadRuntime{
		ledger: ledger.CLIConfig{Resources: resources.Ledger(), Transport: transport, Now: resources.Now, Pacer: resources.MockReadPacer(), Stderr: os.Stderr},
		market: market.CLIConfig{Resources: resources.Market(), CanonicalInstrumentID: ledger.ComposeIdentifiers(), Transport: transport, Now: resources.Now, Pacer: resources.MockReadPacer()},
		close:  closeTransport,
	}
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "kiwoom-mock-candles" || args[0] == "kiwoom-mock-sync" || args[0] == "paper-run-loop" || args[0] == "paper-execute" || args[0] == "paper-execute-stream" || args[0] == "paper-import-bars" || args[0] == "paper-init" || args[0] == "paper-input-status" || args[0] == "paper-improve" || args[0] == "paper-promote" || args[0] == "paper-promotion-status" || args[0] == "paper-promotion-abort" || args[0] == "ledger-export" || args[0] == "benchmark-import" || args[0] == "benchmark-status" || args[0] == "owner-fx-import" || args[0] == "owner-price-import") {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runContext(ctx, args); err != nil && !(args[0] == "paper-run-loop" && errors.Is(err, context.Canceled)) {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if err := run(args); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runContext(context.Background(), args)
}

func runContext(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: omni-core <migrate|ledger-replay-preview|ledger-replay-activate|ledger-export|benchmark-import|benchmark-status|owner-fx-import|owner-price-import|kiwoom-mock-sync|kiwoom-mock-candles|serve|backup|verify-restore|strategy-register|strategy-status|strategy-select|strategy-rollback|paper-run-due|paper-run-loop|paper-init|paper-import-bars|paper-execute|paper-execute-stream|paper-input-status|paper-improve|paper-promote|paper-promotion-status|paper-promotion-abort>")
	}
	resources := composition.NewResources()
	switch args[0] {
	case "owner-price-import", "owner-fx-import", "benchmark-import", "benchmark-status", "kiwoom-mock-candles":
		runtime := mockReadRuntimeFactory(resources)
		defer runtime.close()
		return market.RunCLI(ctx, args[0], args[1:], runtime.market, os.Stdout)
	case "ledger-replay-preview", "ledger-replay-activate", "ledger-export", "kiwoom-mock-sync":
		runtime := mockReadRuntimeFactory(resources)
		defer runtime.close()
		return ledger.RunCLI(ctx, args[0], args[1:], runtime.ledger, os.Stdout)
	case "migrate", "backup", "verify-restore":
		return maintenance.RunCLI(ctx, args[0], args[1:], resources.Maintenance(), os.Stderr)
	case "strategy-register", "strategy-status", "strategy-select", "strategy-rollback", "paper-run-due", "paper-run-loop", "paper-init", "paper-import-bars", "paper-execute", "paper-execute-stream", "paper-input-status", "paper-improve", "paper-promote", "paper-promotion-status", "paper-promotion-abort":
		return trading.RunCLI(ctx, args[0], args[1:], resources.Trading(), os.Stdin, os.Stdout)
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
		if err := composition.ValidateAllowedOrigin(*allowOrigin); err != nil {
			return err
		}
		runtime, err := resources.OpenServer(*dbPath, *marketFixture, *researchFeed)
		if err != nil {
			return err
		}
		defer runtime.Close()
		srv := &http.Server{
			Addr:              *addr,
			Handler:           composition.WithCORS(runtime.Routes(), *allowOrigin),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		log.Printf("omni-core listening on %s", *addr)
		return srv.ListenAndServe()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
