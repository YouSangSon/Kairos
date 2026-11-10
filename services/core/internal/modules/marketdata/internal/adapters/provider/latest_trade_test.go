package provider

import (
	"context"
	app "omni-folio/services/core/internal/modules/marketdata/internal/application"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"reflect"
	"testing"
)

func TestLatestTradeMappingPreservesExactIdentityAndEvidence(t *testing.T) {
	value := sdk.KiwoomLatestTrade{Source: "kiwoom", Environment: sdk.KiwoomProduction, Exchange: sdk.KiwoomKRX, Symbol: "005930", Currency: "KRW", Price: "1250", ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z"}
	want := app.LatestTrade{Source: "kiwoom", Environment: "production", Exchange: "KRX", Symbol: "005930", Currency: "KRW", Price: "1250", ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z"}
	if got := tradeValue(value); !reflect.DeepEqual(got, want) {
		t.Fatalf("provider mapping changed trade: %+v", got)
	}
	if _, err := (TradeSource{}).LatestTrade(context.Background(), "005930"); err == nil {
		t.Fatal("nil SDK client was accepted")
	}
}
