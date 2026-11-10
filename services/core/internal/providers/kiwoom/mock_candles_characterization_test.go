package kiwoom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const captureDailyRow = `{"dt":"20260824","cur_prc":"-001,250.00","open_pric":"+001200","high_pric":"1300","low_pric":"-1150","trde_qty":"+001000"}`

func mockSyncNow() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
func mockSyncPacer() *MockReadPacer {
	clock := mockSyncNow()
	return NewMockReadPacer(func() time.Time { return clock }, func(ctx context.Context, delay time.Duration) error { clock = clock.Add(delay); return ctx.Err() })
}
func TestKiwoomMockCandlesTransportKeepsSeparateCapabilities(t *testing.T) {
	var calls atomic.Int32
	base := kiwoomRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return kiwoomResponse(200, nil, "{}"), nil
	})
	chart := &mockCandleTransport{base, mockSyncPacer()}
	for _, request := range []struct{ method, target, api string }{
		{http.MethodGet, kiwoomMockBase + kiwoomChartPath, "ka10081"},
		{http.MethodPost, kiwoomProductionBase + kiwoomTokenPath, ""},
		{http.MethodPost, "http://mockapi.kiwoom.com" + kiwoomChartPath, "ka10081"},
		{http.MethodPost, kiwoomMockBase + kiwoomAccountPath, "ka00001"},
		{http.MethodPost, kiwoomMockBase + kiwoomAccountPath, "kt00018"},
		{http.MethodPost, kiwoomMockBase + kiwoomAccountPath, "ka10075"},
		{http.MethodPost, kiwoomMockBase + kiwoomOrderPath, "kt10000"},
		{http.MethodPost, kiwoomMockBase + kiwoomChartPath, "ka10079"},
		{http.MethodPost, kiwoomMockBase + kiwoomChartPath + "?private=1", "ka10081"},
		{http.MethodPost, kiwoomMockBase + kiwoomChartPath + "?", "ka10081"},
		{http.MethodPost, "https://private@mockapi.kiwoom.com" + kiwoomChartPath, "ka10081"},
		{http.MethodPost, kiwoomMockBase + kiwoomTokenPath, "ka10081"},
	} {
		r, _ := http.NewRequest(request.method, request.target, nil)
		r.Header.Set("api-id", request.api)
		if _, err := chart.RoundTrip(r); err == nil {
			t.Fatalf("chart transport allowed %s %s %s", request.method, request.target, request.api)
		}
	}
	account := &mockSnapshotTransport{base, mockSyncPacer()}
	for _, api := range []string{"ka10080", "ka10081"} {
		r, _ := http.NewRequest(http.MethodPost, kiwoomMockBase+kiwoomChartPath, nil)
		r.Header.Set("api-id", api)
		if _, err := account.RoundTrip(r); err == nil {
			t.Fatal("existing account caller acquired chart authority")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("disallowed request reached base transport")
	}
}

func TestKiwoomCandleBatchKeepsRawTimesPairedAfterLatestBarCap(t *testing.T) {
	newest := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]kiwoomCandleRow, maxMarketDataRows+1)
	for i := range rows {
		price := fmt.Sprint(i + 1)
		rows[i] = kiwoomCandleRow{Date: newest.AddDate(0, 0, -i).Format("20060102"), Open: price, High: price, Low: price, Close: price, Volume: "1"}
	}
	raw, err := json.Marshal(kiwoomCandleResponse{kiwoomResult: kiwoomResult{ReturnCode: new(int)}, StockCode: "005930", DailyBars: rows})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := onePageCandleClient(t, string(raw)).readCandleBatch(context.Background(), "005930", "1d")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.bars) != maxMarketDataRows {
		t.Fatalf("batch rows=%d", len(batch.bars))
	}
	for i, bar := range batch.bars {
		row := rows[maxMarketDataRows-1-i]
		if bar.ProviderTime != row.Date || bar.Close != row.Close {
			t.Fatal("latest bounded selection detached raw time from its values")
		}
	}
}

func candleCaptureTLS(t *testing.T, chart http.HandlerFunc) (http.RoundTripper, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		api := r.Header.Get("api-id")
		if r.Method != http.MethodPost {
			t.Error("unexpected provider method")
		}
		if r.URL.Path == kiwoomTokenPath && api == "" {
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]string{"grant_type": "client_credentials", "appkey": "synthetic-app-key", "secretkey": "synthetic-secret-key"}) {
				t.Error("synthetic OAuth body differs")
			}
			io.WriteString(w, strings.Replace(syntheticTokenJSON("synthetic-token"), "20300102000000", "20310102000000", 1))
			return
		}
		if r.URL.Path != kiwoomChartPath || (api != "ka10080" && api != "ka10081") || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("unexpected chart capability or authorization")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("api-id", api)
		chart(w, r)
	}))
	t.Cleanup(server.Close)
	base := server.Client().Transport.(*http.Transport)
	t.Cleanup(base.CloseIdleConnections)
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return kiwoomRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "mockapi.kiwoom.com" {
			return nil, errors.New("test transport refuses non-mock origin")
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host, request.Host = local.Scheme, local.Host, local.Host
		return base.RoundTrip(request)
	}), calls
}

func TestLegacyCandlesKeepClockAndFractionalTimeBehavior(t *testing.T) {
	for _, fraction := range []string{"", ".123", ",123"} {
		t.Run(fraction, func(t *testing.T) {
			var fetched atomic.Bool
			transport, _ := candleCaptureTLS(t, func(w http.ResponseWriter, r *http.Request) {
				fetched.Store(true)
				if fraction == "" {
					io.WriteString(w, candleDailyBody(captureDailyRow))
					return
				}
				row := strings.Replace(captureDailyRow, `"dt":"20260824"`, `"cntr_tm":"20260824103000`+fraction+`"`, 1)
				fmt.Fprintf(w, `{"stk_cd":"005930","stk_min_pole_chart_qry":[%s],"return_code":0}`, row)
			})
			client := newSyntheticClient(t, KiwoomMock, transport)
			client.now = func() time.Time {
				if fetched.Load() {
					return mockSyncNow().Add(-time.Second)
				}
				return mockSyncNow()
			}
			interval, want := "1d", "2026-08-23T15:00:00Z"
			if fraction != "" {
				interval, want = "1m", "2026-08-24T01:30:00Z"
			}
			series, err := client.Candles(context.Background(), "005930", interval)
			if err != nil || len(series.Bars) != 1 || series.Bars[0].At != want || series.PriceAdjustment != marketDataAdjustmentProviderAdjusted {
				t.Fatalf("legacy candle behavior changed: %+v %v", series, err)
			}
		})
	}
}

func TestKiwoomMockCandlesPacesOAuthRefreshPagesAndAccountCaller(t *testing.T) {
	var unauthorized atomic.Bool
	transport, _ := candleCaptureTLS(t, func(w http.ResponseWriter, r *http.Request) {
		if unauthorized.CompareAndSwap(false, true) {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("next-key") == "" {
			w.Header().Set("cont-yn", "Y")
			w.Header().Set("next-key", "page-two")
		}
		io.WriteString(w, candleDailyBody(captureDailyRow))
	})
	clock := mockSyncNow()
	var starts []time.Time
	pacer := NewMockReadPacer(func() time.Time { return clock }, func(ctx context.Context, delay time.Duration) error {
		clock = clock.Add(delay)
		return ctx.Err()
	})
	base := kiwoomRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		starts = append(starts, clock)
		clock = clock.Add(200 * time.Millisecond)
		return transport.RoundTrip(r)
	})
	for i := 0; i < 2; i++ {
		if err := captureRun(context.Background(), filepath.Join(t.TempDir(), "capture.json"), "1d", io.Discard, base, mockSyncNow, pacer); err != nil {
			t.Fatal(err)
		}
	}
	account := &mockSnapshotTransport{base, pacer}
	r, _ := http.NewRequest(http.MethodPost, kiwoomMockBase+kiwoomTokenPath, strings.NewReader(`{"grant_type":"client_credentials","appkey":"synthetic-app-key","secretkey":"synthetic-secret-key"}`))
	response, err := account.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(starts) != 9 {
		t.Fatalf("refresh/pages/two commands/account OAuth calls=%d", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < time.Second {
			t.Fatal("mock commands did not share process-local pacing")
		}
	}
}

func captureRun(ctx context.Context, _ string, interval string, _ io.Writer, transport http.RoundTripper, now func() time.Time, pacer *MockReadPacer) error {
	client, err := NewMockCandles(MockCandlesConfig{AppKey: "synthetic-app-key", SecretKey: "synthetic-secret-key", AliasKey: []byte("0123456789abcdef0123456789abcdef"), Transport: transport, Now: now, Pacer: pacer})
	if err != nil {
		return err
	}
	defer client.Close()
	_, err = client.Capture(ctx, "005930", interval)
	return err
}
