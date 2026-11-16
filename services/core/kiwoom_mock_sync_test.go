package main

import (
	"context"
	"strings"
	"testing"
)

func TestKiwoomMockSyncDispatchRequiresExplicitArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"-db", "missing.db"}, {"-ledger-account", "account-main"},
		{"-db", "missing.db", "-ledger-account", "other"},
		{"-db", "missing.db", "-ledger-account", "account-main", "-mode", "production"},
		{"-db", "missing.db", "-ledger-account", "account-main", "extra"},
	} {
		err := runContext(context.Background(), append([]string{"kiwoom-mock-sync"}, args...))
		if err == nil || !strings.Contains(err.Error(), "requires only -db and -ledger-account account-main") {
			t.Fatalf("args=%v: expected explicit mock-only argument validation, got %v", args, err)
		}
	}
}
