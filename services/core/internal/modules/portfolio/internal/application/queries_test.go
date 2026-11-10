package application

import (
	"context"
	"errors"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"testing"
	"time"
)

type inputReader func(context.Context, c.InputQuery) (c.Inputs, error)

func (f inputReader) ReadValuationInputs(ctx context.Context, q c.InputQuery) (c.Inputs, error) {
	return f(ctx, q)
}
func TestQueriesPreserveInputFailureClosureAndRequestedCutoff(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stop := errors.New("observed query")
	tests := []struct {
		name string
		run  func(*Queries) error
		want c.InputQuery
	}{
		{"cash", func(q *Queries) error {
			_, err := q.CashValuation(context.Background(), "KRW", at.Format(time.RFC3339Nano))
			return err
		}, c.InputQuery{At: &at, FX: true}},
		{"holding", func(q *Queries) error {
			_, err := q.HoldingValuation(context.Background(), at.Format(time.RFC3339Nano))
			return err
		}, c.InputQuery{At: &at, Prices: true}},
		{"current", func(q *Queries) error { _, err := q.CurrentValuation(context.Background()); return err }, c.InputQuery{FX: true, Prices: true}},
		{"XIRR", func(q *Queries) error { _, err := q.XIRR(context.Background()); return err }, c.InputQuery{FX: true, Prices: true}},
		{"TWR", func(q *Queries) error { _, err := q.TWR(context.Background()); return err }, c.InputQuery{FX: true, Prices: true}},
		{"checkpoints", func(q *Queries) error { _, err := q.TWRCheckpoints(context.Background()); return err }, c.InputQuery{FX: true, Prices: true}},
		{"benchmarks", func(q *Queries) error { _, err := q.Benchmarks(context.Background()); return err }, c.InputQuery{FX: true, Prices: true, Benchmarks: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &Queries{Now: func() time.Time { return at }, Inputs: inputReader(func(_ context.Context, got c.InputQuery) (c.Inputs, error) {
				if got.FX != tc.want.FX || got.Prices != tc.want.Prices || got.Benchmarks != tc.want.Benchmarks || (got.At == nil) != (tc.want.At == nil) || (got.At != nil && !got.At.Equal(*tc.want.At)) {
					t.Fatalf("proof scope changed: %+v", got)
				}
				return c.Inputs{}, stop
			})}
			if err := tc.run(q); !errors.Is(err, stop) {
				t.Fatal(err)
			}
		})
	}
	q := &Queries{Now: func() time.Time { return at }, Inputs: inputReader(func(_ context.Context, query c.InputQuery) (c.Inputs, error) {
		all := c.Inputs{Cutoff: at.Add(time.Nanosecond)}
		all.Market.Cutoff = all.Cutoff
		return all, nil
	})}
	if value, err := q.HoldingValuation(context.Background(), at.Format(time.RFC3339Nano)); err == nil || value != nil {
		t.Fatal("different requested cutoff accepted")
	}
	q.Inputs = inputReader(func(context.Context, c.InputQuery) (c.Inputs, error) {
		all := c.Inputs{Cutoff: at}
		all.Market.Cutoff = at.Add(time.Nanosecond)
		return all, nil
	})
	if value, err := q.CurrentValuation(context.Background()); err == nil || value != nil {
		t.Fatal("mixed market cutoff accepted")
	}
}

func TestRequestedPreservesCanonicalUTCContract(t *testing.T) {
	q := &Queries{Now: func() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) }}
	for _, kind := range []string{"cash", "holding"} {
		for _, tc := range []struct {
			raw   string
			valid bool
		}{
			{"2026-01-01T00:00:00Z", true}, {"2026-01-01T00:00:00.123456789Z", true},
			{"2026-01-01T00:00:00+00:00", false}, {"2026-01-01T00:00:00.100Z", false},
			{"2026-01-01T00:00:00", false}, {"2026-01-01T00:00:00z", false}, {"2026-02-30T00:00:00Z", false}, {"", false},
		} {
			t.Run(kind+"/"+tc.raw, func(t *testing.T) {
				at, err := q.requested(tc.raw, kind)
				if tc.valid {
					if err != nil || at.UTC().Format(time.RFC3339Nano) != tc.raw {
						t.Fatalf("time=%v err=%v", at, err)
					}
					return
				}
				var failure *c.Failure
				if !errors.As(err, &failure) || failure.Code != "invalid_query" || failure.Conflict {
					t.Fatalf("failure changed: %v", err)
				}
				message, field := "cash valuation query values are invalid", ""
				if kind == "holding" {
					message, field = "holding valuation as_of is invalid", "as_of"
				}
				if failure.Message != message || failure.Field != field {
					t.Fatalf("failure changed: %+v", failure)
				}
			})
		}
	}
}
