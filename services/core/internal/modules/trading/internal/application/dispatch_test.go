package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type dispatchRecorder struct {
	t                                     *testing.T
	calls                                 []string
	preflightErr, authorizeErr, submitErr error
	dispatched                            bool
	unknown                               *OrderState
	outcome                               OrderEvent
}

func (p *dispatchRecorder) Provider() string { return "kiwoom" }
func (p *dispatchRecorder) Mode() string     { return "synthetic" }
func (p *dispatchRecorder) Preflight(context.Context) error {
	p.calls = append(p.calls, "preflight")
	return p.preflightErr
}
func (p *dispatchRecorder) LoadIntent(_ context.Context, id string) (OrderIntent, error) {
	p.t.Helper()
	if id != "order_dispatch_test" {
		p.t.Fatalf("load order=%q", id)
	}
	p.calls = append(p.calls, "load")
	return OrderIntent{ClientOrderID: "client_dispatch_test", Provider: "kiwoom", Mode: "synthetic", Symbol: "005930", Side: "BUY", Quantity: "2", LimitPrice: "1000"}, nil
}
func (p *dispatchRecorder) AuthorizeOnce(_ context.Context, id string, fence int64) (*OrderState, bool, error) {
	p.t.Helper()
	if id != "order_dispatch_test" || fence != 7 {
		p.t.Fatalf("authorization order=%q fence=%d", id, fence)
	}
	p.calls = append(p.calls, "authorize")
	return p.unknown, p.dispatched, p.authorizeErr
}
func (p *dispatchRecorder) SubmitLimitOrder(_ context.Context, intent OrderIntent) (string, error) {
	p.t.Helper()
	if intent.ClientOrderID != "client_dispatch_test" || intent.Symbol != "005930" || intent.Quantity != "2" || intent.LimitPrice != "1000" {
		p.t.Fatalf("submitted intent=%+v", intent)
	}
	p.calls = append(p.calls, "submit")
	return "kiwoom_order_AAAAAAAAAAAAAAAAAAAAAAAA", p.submitErr
}
func (p *dispatchRecorder) RecordOutcome(_ context.Context, event OrderEvent) (*OrderState, error) {
	p.calls = append(p.calls, "record")
	p.outcome = event
	return &OrderState{OrderID: event.OrderID, Status: "OPEN"}, nil
}

func TestSyntheticLimitDispatchSequenceAndUnknownOutcome(t *testing.T) {
	denied := errors.New("authorization denied")
	for _, test := range []struct {
		name                                  string
		preflightErr, authorizeErr, submitErr error
		dispatched                            bool
		calls                                 []string
		wantErr                               string
	}{
		{name: "preflight precedes durable authorization and submit", dispatched: true, calls: []string{"load", "preflight", "authorize", "submit", "record"}},
		{name: "preflight failure never authorizes", preflightErr: errors.New("unavailable"), calls: []string{"load", "preflight"}, wantErr: "preflight failed"},
		{name: "authorization failure never submits", authorizeErr: denied, calls: []string{"load", "preflight", "authorize"}, wantErr: denied.Error()},
		{name: "already dispatched never resubmits", calls: []string{"load", "preflight", "authorize"}, wantErr: "already dispatched"},
		{name: "timeout preserves unknown state without recording outcome", dispatched: true, submitErr: context.DeadlineExceeded, calls: []string{"load", "preflight", "authorize", "submit"}, wantErr: "outcome is unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &dispatchRecorder{t: t, preflightErr: test.preflightErr, authorizeErr: test.authorizeErr, submitErr: test.submitErr, dispatched: test.dispatched, unknown: &OrderState{OrderID: "order_dispatch_test", Status: "SUBMIT_UNKNOWN"}}
			result, err := SubmitSyntheticLimit(context.Background(), probe, func(prefix string) string {
				if prefix != "order_event" {
					t.Fatalf("ID prefix=%q", prefix)
				}
				return "event_dispatch_test"
			}, probe, "order_dispatch_test", 7)
			if !reflect.DeepEqual(probe.calls, test.calls) {
				t.Fatalf("calls=%v want=%v", probe.calls, test.calls)
			}
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("result=%+v err=%v want=%q", result, err, test.wantErr)
				}
				if test.authorizeErr != nil && !errors.Is(err, denied) {
					t.Fatalf("authorization cause lost: %v", err)
				}
				if test.submitErr != nil || (!test.dispatched && test.authorizeErr == nil && test.preflightErr == nil) {
					if result != probe.unknown || result.Status != "SUBMIT_UNKNOWN" {
						t.Fatalf("durable unknown state changed: %+v", result)
					}
				}
				if probe.outcome != (OrderEvent{}) {
					t.Fatalf("unexpected outcome=%+v", probe.outcome)
				}
				return
			}
			if err != nil || result == nil || result.Status != "OPEN" {
				t.Fatalf("acknowledged state=%+v err=%v", result, err)
			}
			want := OrderEvent{EventID: "event_dispatch_test", OrderID: "order_dispatch_test", Type: "SUBMIT_ACKNOWLEDGED", Source: "reconciliation", ProviderOrderRef: "kiwoom_order_AAAAAAAAAAAAAAAAAAAAAAAA"}
			if probe.outcome != want {
				t.Fatalf("outcome=%+v want=%+v", probe.outcome, want)
			}
		})
	}
}
