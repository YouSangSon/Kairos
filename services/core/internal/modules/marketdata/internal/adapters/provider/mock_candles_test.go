package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "omni-folio/services/core/internal/providers/kiwoom"
)

func TestCapturePagesPreservesOnlyLocalVisitorError(t *testing.T) {
	upstream := errors.New("private upstream detail")
	local := errors.New("cannot store validated candle page")
	for _, source := range []string{"Keychain", "transport", "visitor"} {
		t.Run(source, func(t *testing.T) {
			now := func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
			config := Config{
				ReadSecret: func(context.Context) ([]byte, error) {
					if source == "Keychain" {
						return nil, upstream
					}
					return []byte(`{"schema_version":"kiwoom-mock-read.v1","app_key":"synthetic-app","secret_key":"synthetic-key","alias_key_base64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","account_number":"1234567890"}`), nil
				},
				Now:   now,
				Pacer: sdk.NewMockReadPacer(now, func(ctx context.Context, _ time.Duration) error { return ctx.Err() }),
				Transport: candleTestTransport(func(r *http.Request) (*http.Response, error) {
					if source == "transport" {
						return nil, upstream
					}
					body := `{"return_code":0,"token":"synthetic-token","token_type":"Bearer","expires_dt":"20300102000000"}`
					if r.Header.Get("api-id") == "ka10081" {
						body = `{"return_code":0,"stk_cd":"005930","stk_dt_pole_chart_qry":[{"dt":"20260824","cur_prc":"10","open_pric":"10","high_pric":"10","low_pric":"10","trde_qty":"5"}]}`
					}
					return &http.Response{StatusCode: http.StatusOK, Header: r.Header.Clone(), Body: io.NopCloser(strings.NewReader(body))}, nil
				}),
			}
			_, err := CapturePages(context.Background(), "005930", "1d", config, now(), func(sdk.MockCandlePage) error {
				if source != "visitor" {
					t.Fatal("upstream failure reached the visitor")
				}
				return local
			})
			if source == "visitor" {
				if err != local {
					t.Fatalf("visitor error lost identity: %v", err)
				}
			} else if err == nil || errors.Is(err, upstream) || strings.Contains(err.Error(), upstream.Error()) {
				t.Fatalf("expected sanitized upstream failure: %v", err)
			}
		})
	}
}

type candleTestTransport func(*http.Request) (*http.Response, error)

func (transport candleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}
