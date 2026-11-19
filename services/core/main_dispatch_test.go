package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestModularMainRejectsFlagsBeforeOpeningResources(t *testing.T) {
	for _, command := range []string{"migrate", "ledger-replay-preview", "ledger-replay-activate", "ledger-export", "benchmark-import", "benchmark-status", "owner-fx-import", "owner-price-import", "kiwoom-mock-sync", "kiwoom-mock-candles", "kiwoom-mock-candles-resume", "serve", "backup", "verify-restore", "strategy-register", "strategy-status", "strategy-select", "strategy-rollback", "paper-run-due", "paper-run-loop", "paper-init", "paper-import-bars", "paper-execute", "paper-execute-stream", "paper-input-status", "paper-improve", "paper-promote", "paper-promotion-status", "paper-promotion-abort"} {
		t.Run(command, func(t *testing.T) {
			if err := runContext(context.Background(), []string{command, "-h"}); err == nil || strings.Contains(err.Error(), "unknown command") {
				t.Fatal("help entered command resources")
			}
		})
	}
	if err := runContext(context.Background(), []string{"unknown"}); err == nil || err.Error() != `unknown command "unknown"` {
		t.Fatalf("unknown command changed: %v", err)
	}
}
func TestModularMainMigrationAndReplayUseSameDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	if err := runContext(context.Background(), []string{"migrate", "-db", path}); err != nil {
		t.Fatal(err)
	}
	if err := runContext(context.Background(), []string{"ledger-replay-preview", "-db", path}); err == nil || err.Error() != "ledger does not require a legacy replay transition" {
		t.Fatalf("genesis replay policy changed: %v", err)
	}
	if err := runContext(context.Background(), []string{"serve", "-db", path, "-market-fixture", "missing-fixture"}); err == nil || !strings.Contains(err.Error(), "open market fixture") {
		t.Fatalf("server did not reach module fixture: %v", err)
	}
}
