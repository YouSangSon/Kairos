package kiwoom

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestLatestTradeCaptureProtocolFailureKeepsSingleOAuthAndChartCall(t *testing.T) {
	script := &kiwoomScript{t: t, steps: []func(*http.Request) *http.Response{
		func(*http.Request) *http.Response {
			return kiwoomResponse(http.StatusOK, nil, syntheticTokenJSON("token"))
		},
		func(request *http.Request) *http.Response {
			assertKiwoomRequest(t, request, kiwoomChartPath, "ka10079")
			return kiwoomResponse(http.StatusInternalServerError, nil, `{}`)
		},
	}}
	client := newSyntheticClient(t, KiwoomProduction, script)
	if observation, err := client.LatestTrade(context.Background(), "005930"); err == nil || observation != nil || script.calls != 2 {
		t.Fatalf("provider failure was retried: observation=%+v calls=%d err=%v", observation, script.calls, err)
	}
}
func TestLatestTradeCaptureProtocolKeepsExactTradeAndClock(t *testing.T) {
	row := `{"cur_prc":"1250","trde_qty":"5","cntr_tm":"20260824103000","open_pric":"1200","high_pric":"1300","low_pric":"1150"}`
	client := onePageLatestTradeClient(t, tradeBody("005930", row))
	client.now = func() time.Time { return time.Date(2026, 8, 24, 1, 30, 1, 0, time.UTC) }
	trade, err := client.LatestTrade(context.Background(), "005930")
	if err != nil || trade.Source != "kiwoom" || trade.Environment != KiwoomProduction || trade.Exchange != KiwoomKRX || trade.Symbol != "005930" || trade.Currency != "KRW" || trade.Price != "1250" || trade.ObservedAt != "2026-08-24T01:30:00Z" || trade.FetchedAt != "2026-08-24T01:30:01Z" {
		t.Fatalf("native capture source changed: %+v %v", trade, err)
	}
}
