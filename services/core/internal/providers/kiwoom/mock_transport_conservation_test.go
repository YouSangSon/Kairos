// Conserved from kiwoom_mock_sync_test.go at 715c95d709dce1aa3e0c7ca93c002231cd9d4eea.
package kiwoom

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestKiwoomMockSyncTransportAllowlistAndCancellation(t *testing.T) {
	var calls atomic.Int32
	transport := &mockSnapshotTransport{base: kiwoomRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return kiwoomResponse(200, nil, "{}"), nil
	}), pacer: conservationMockPacer()}
	for _, target := range []string{"https://api.kiwoom.com/oauth2/token", "http://mockapi.kiwoom.com/oauth2/token", "https://mockapi.kiwoom.com/api/dostk/ordr", "https://mockapi.kiwoom.com/oauth2/token?secret=private"} {
		req, _ := http.NewRequest(http.MethodPost, target, nil)
		if _, err := transport.RoundTrip(req); err == nil {
			t.Fatal("transport accepted an unapproved endpoint")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, kiwoomMockBase+kiwoomTokenPath, nil)
	if _, err := transport.RoundTrip(req); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("cancelled/invalid requests reached network: calls=%d err=%v", calls.Load(), err)
	}
	pacer := NewMockReadPacer(time.Now, WaitMockRead)
	pacer.gate <- struct{}{}
	transport.pacer = pacer
	if _, err := transport.RoundTrip(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("pacer lock ignored context cancellation: %v", err)
	}
	<-pacer.gate
	pacer.next = time.Now().Add(time.Hour)
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, kiwoomMockBase+kiwoomTokenPath, nil)
	if _, err := transport.RoundTrip(req); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 0 || len(pacer.gate) != 0 {
		t.Fatalf("pacer wait leaked gate or network request: %v", err)
	}
}

func conservationMockPacer() *MockReadPacer {
	clock := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	return NewMockReadPacer(func() time.Time { return clock }, func(ctx context.Context, d time.Duration) error { clock = clock.Add(d); return ctx.Err() })
}
