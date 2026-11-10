package cli

import (
	"context"
	"errors"
	"io"
	"omni-folio/services/core/internal/modules/marketdata/construction"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"testing"
)

type resourceStub struct {
	opens   int
	mode    string
	session *sessionStub
}

func (r *resourceStub) OpenReadOnly(context.Context, string) (construction.Session, error) {
	r.opens++
	r.mode = "read-only"
	return r.session, nil
}
func (r *resourceStub) OpenReadWrite(context.Context, string) (construction.Session, error) {
	r.opens++
	r.mode = "read-write"
	return r.session, nil
}

type sessionStub struct{ closed int }

func (s *sessionStub) Imports() c.Imports              { return &statusStub{} }
func (s *sessionStub) OwnerPrice() c.OwnerPriceImports { return nil }
func (s *sessionStub) Close() error                    { s.closed++; return nil }

type statusStub struct{ c.Imports }

func (*statusStub) BenchmarkStatus(context.Context, string, string) (*c.BenchmarkSeriesReceipt, error) {
	return nil, errors.New("private storage details")
}
func TestMarketCLIRejectsArgumentsBeforeOpeningResources(t *testing.T) {
	for _, command := range []string{"owner-price-import", "owner-fx-import", "benchmark-import", "benchmark-status"} {
		for _, args := range [][]string{nil, {"-h"}, {"-invalid"}, {"-db", "missing", "-input", "missing"}} {
			r := &resourceStub{}
			if err := Run(context.Background(), command, args, Config{Resources: r}, io.Discard); err == nil || r.opens != 0 {
				t.Fatalf("%s %v opened resources: %v", command, args, err)
			}
		}
	}
}
func TestBenchmarkStatusUsesReadOnlyAndClosesOnRedactedFailure(t *testing.T) {
	s := &sessionStub{}
	r := &resourceStub{session: s}
	err := Run(context.Background(), "benchmark-status", []string{"-db", "existing", "-benchmark-id", "sample"}, Config{Resources: r}, io.Discard)
	if err == nil || err.Error() != "benchmark status could not verify the index version" || r.mode != "read-only" || r.opens != 1 || s.closed != 1 {
		t.Fatalf("err=%v mode=%s opens=%d closed=%d", err, r.mode, r.opens, s.closed)
	}
}
