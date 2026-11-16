package kiwoom

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMockSnapshotConstructionLifetimeAndSharedBudget(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	var delays []time.Duration
	pacer := NewMockReadPacer(clock, func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		now = now.Add(delay)
		return ctx.Err()
	})
	calls := 0
	base := kiwoomRoundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("synthetic failure") })
	key := bytes.Repeat([]byte{7}, 32)
	config := MockSnapshotConfig{AppKey: "app", SecretKey: "secret", AccountNumber: "9876543210", AliasKey: key, Transport: base, Now: clock, Pacer: pacer}
	first, err := NewMockSnapshot(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMockSnapshot(config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if calls != 0 || !strings.HasPrefix(first.AccountRef(), "kiwoom_account_") || first.AccountRef() != second.AccountRef() {
		t.Fatal("construction used network or changed account alias")
	}
	key[0] = 99
	owned := first.client.aliasKey
	if owned[0] != 7 {
		t.Fatal("client borrowed input alias bytes")
	}
	if _, err := first.Snapshot(context.Background()); err == nil || calls != 1 {
		t.Fatalf("snapshot did not attempt fixed read: calls=%d", calls)
	}
	first.Close()
	first.Close()
	if !bytes.Equal(owned, make([]byte, 32)) {
		t.Fatal("closed client retained owned alias bytes")
	}
	if _, err := first.Snapshot(context.Background()); err == nil || calls != 1 {
		t.Fatal("closed snapshot reader reached network")
	}
	if _, err := second.Snapshot(context.Background()); err == nil || calls != 2 || len(delays) != 1 || delays[0] != time.Second {
		t.Fatalf("close reset borrowed transport or shared pacing: calls=%d delays=%v", calls, delays)
	}
	candles, err := NewMockCandles(MockCandlesConfig{AppKey: "app", SecretKey: "secret", AliasKey: key, Transport: base, Now: clock, Pacer: pacer})
	if err != nil {
		t.Fatal(err)
	}
	defer candles.Close()
	if _, err := candles.Capture(context.Background(), "005930", "1d"); err == nil || calls != 3 || len(delays) != 2 || delays[1] != time.Second {
		t.Fatalf("candle reader escaped the snapshot budget: calls=%d delays=%v", calls, delays)
	}
	candles.Close()
	if _, err := candles.Capture(context.Background(), "005930", "1d"); err == nil || calls != 3 {
		t.Fatal("closed candle reader reached network")
	}
	config.Pacer = nil
	if _, err := NewMockSnapshot(config); err == nil {
		t.Fatal("nil pacer silently created a separate budget")
	}
}

func TestMockSnapshotTransportRejectsWritesBeforeBudget(t *testing.T) {
	calls, waits := 0, 0
	pacer := NewMockReadPacer(time.Now, func(context.Context, time.Duration) error { waits++; return nil })
	reader, err := NewMockSnapshot(MockSnapshotConfig{AppKey: "app", SecretKey: "secret", AccountNumber: "9876543210", AliasKey: make([]byte, 32), Now: time.Now, Pacer: pacer, Transport: kiwoomRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, target := range []string{"https://api.kiwoom.com/api/dostk/acnt", "https://mockapi.kiwoom.com/api/dostk/ordr", "https://mockapi.kiwoom.com/api/dostk/acnt?extra=1"} {
		r, _ := http.NewRequest(http.MethodPost, target, nil)
		r.Header.Set("api-id", "kt10000")
		if _, err := reader.client.transport.RoundTrip(r); err == nil {
			t.Fatal("unexpected read capability accepted")
		}
	}
	if calls != 0 || waits != 0 {
		t.Fatal("forbidden transport consumed budget or reached network")
	}
}
