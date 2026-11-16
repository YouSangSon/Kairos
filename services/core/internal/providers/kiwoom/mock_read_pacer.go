package kiwoom

import (
	"context"
	"net/http"
	"time"
)

type MockReadPacer struct {
	gate chan struct{}
	next time.Time
	now  func() time.Time
	wait func(context.Context, time.Duration) error
}

func NewMockReadPacer(now func() time.Time, wait func(context.Context, time.Duration) error) *MockReadPacer {
	if now == nil || wait == nil {
		return nil
	}
	return &MockReadPacer{gate: make(chan struct{}, 1), now: now, wait: wait}
}
func (p *MockReadPacer) valid() bool {
	return p != nil && p.gate != nil && p.now != nil && p.wait != nil
}
func WaitMockRead(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (p *MockReadPacer) roundTrip(request *http.Request, base http.RoundTripper) (*http.Response, error) {
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case p.gate <- struct{}{}:
	}
	defer func() { <-p.gate }()
	if delay := p.next.Sub(p.now()); delay > 0 {
		if err := p.wait(request.Context(), delay); err != nil {
			return nil, err
		}
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	response, err := base.RoundTrip(request)
	p.next = p.now().Add(time.Second)
	return response, err
}
