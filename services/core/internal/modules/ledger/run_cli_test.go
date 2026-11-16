package ledger_test

import (
	"context"
	"errors"
	"io"
	"omni-folio/services/core/internal/modules/ledger"
	"omni-folio/services/core/internal/modules/ledger/construction"
	"strings"
	"testing"
)

type unopenedLedgerResources struct{ calls *int }

func (r unopenedLedgerResources) OpenExisting(string) (construction.Session, error) {
	*r.calls++
	return nil, errors.New("unexpected open")
}
func (r unopenedLedgerResources) OpenReadOnly(context.Context, string) (construction.Session, error) {
	*r.calls++
	return nil, errors.New("unexpected open")
}
func (r unopenedLedgerResources) OpenReadWrite(context.Context, string) (construction.Session, error) {
	*r.calls++
	return nil, errors.New("unexpected open")
}
func TestCLIRejectsInputBeforeOpeningResources(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"ledger-export", nil}, {"ledger-export", []string{"-h"}},
		{"kiwoom-mock-sync", []string{"-db", "unused", "-ledger-account", "wrong"}},
		{"ledger-replay-preview", []string{"-db", "unused", "-expected-events-sha", "not-allowed"}},
		{"ledger-replay-activate", nil},
	} {
		t.Run(tc.command+strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			err := ledger.RunCLI(context.Background(), tc.command, tc.args, ledger.CLIConfig{Resources: unopenedLedgerResources{&calls}, Stderr: io.Discard}, io.Discard)
			if err == nil || calls != 0 {
				t.Fatalf("invalid command opened resources: %d %v", calls, err)
			}
		})
	}
}
