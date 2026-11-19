package main

import (
	"context"
	"errors"
	"log"
	"omni-folio/services/core/internal/composition"
	"os"
	"os/signal"
	"syscall"
)

var cliOptions composition.CLIOptions

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "serve" || args[0] == "kiwoom-mock-candles" || args[0] == "kiwoom-mock-candles-resume" || args[0] == "kiwoom-mock-sync" || args[0] == "paper-run-loop" || args[0] == "paper-execute" || args[0] == "paper-execute-stream" || args[0] == "paper-import-bars" || args[0] == "paper-init" || args[0] == "paper-input-status" || args[0] == "paper-improve" || args[0] == "paper-promote" || args[0] == "paper-promotion-status" || args[0] == "paper-promotion-abort" || args[0] == "ledger-export" || args[0] == "benchmark-import" || args[0] == "benchmark-status" || args[0] == "owner-fx-import" || args[0] == "owner-price-import") {
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
	return composition.RunCLI(ctx, args, cliOptions, os.Stdin, os.Stdout, os.Stderr)
}
