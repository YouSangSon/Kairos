package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

type marketDataPortFunc func(context.Context, string, string) (*MarketDataSeries, error)

func (f marketDataPortFunc) Candles(ctx context.Context, symbol, interval string) (*MarketDataSeries, error) {
	return f(ctx, symbol, interval)
}

func TestMarketDataCandlesFixtureResponseIsExact(t *testing.T) {
	svc, _ := testService(t, []time.Time{mustTime("2026-01-10T15:01:00Z")}, nil)
	marketData, err := svc.loadMarketDataFixture(fixturePath("market-bars.csv"))
	if err != nil {
		t.Fatal(err)
	}
	svc.marketData = marketData
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/market-data/candles?symbol=AAPL&interval=1d", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	const want = "{\"instrument_id\":null,\"currency\":null,\"bar_time_basis\":\"unspecified\",\"symbol\":\"AAPL\",\"venue\":\"XNAS\",\"timezone\":\"America/New_York\",\"interval\":\"1d\",\"price_adjustment\":\"unspecified\",\"source\":\"local_fixture\",\"sample\":true,\"state\":\"stale\",\"source_as_of\":\"2026-01-07T00:00:00Z\",\"fetched_at\":\"2026-01-10T15:01:00Z\",\"issues\":[{\"code\":\"sample_data\",\"message\":\"market data is a local sample and not live\"}],\"bars\":[{\"at\":\"2026-01-02T00:00:00Z\",\"open\":\"10\",\"high\":\"10.5\",\"low\":\"9.5\",\"close\":\"10\",\"volume\":\"100\"},{\"at\":\"2026-01-03T00:00:00Z\",\"open\":\"11\",\"high\":\"11.75\",\"low\":\"10.5\",\"close\":\"11.5\",\"volume\":\"6\"},{\"at\":\"2026-01-04T00:00:00Z\",\"open\":\"12\",\"high\":\"13\",\"low\":\"11.5\",\"close\":\"12.5\",\"volume\":\"8\"},{\"at\":\"2026-01-05T00:00:00Z\",\"open\":\"13\",\"high\":\"14.5\",\"low\":\"12.75\",\"close\":\"14\",\"volume\":\"20\"},{\"at\":\"2026-01-06T00:00:00Z\",\"open\":\"14\",\"high\":\"15.5\",\"low\":\"13.5\",\"close\":\"15\",\"volume\":\"100\"},{\"at\":\"2026-01-07T00:00:00Z\",\"open\":\"16\",\"high\":\"16.5\",\"low\":\"15.5\",\"close\":\"16\",\"volume\":\"100\"}]}\n"
	if w.Body.String() != want {
		t.Fatalf("unexpected response:\n%s", w.Body.String())
	}
}

func TestMarketDataCandlesErrorsAndQueryValidation(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	request := func(path string) (int, APIError) {
		t.Helper()
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var body APIError
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return w.Code, body
	}
	for _, path := range []string{
		"/v1/market-data/candles?interval=1d",
		"/v1/market-data/candles?symbol=AAPL&symbol=AAPL&interval=1d",
		"/v1/market-data/candles?symbol=AAPL&interval=1d&limit=1",
	} {
		status, body := request(path)
		if status != http.StatusBadRequest || body.Code != "invalid_query" {
			t.Fatalf("path=%s status=%d body=%+v", path, status, body)
		}
	}
	status, body := request("/v1/market-data/candles?symbol=AAPL&interval=1d")
	if status != http.StatusServiceUnavailable || body != (APIError{Code: "market_data_unavailable", Message: "market data is unavailable"}) {
		t.Fatalf("unavailable status=%d body=%+v", status, body)
	}
	marketData, err := svc.loadMarketDataFixture(fixturePath("market-bars.csv"))
	if err != nil {
		t.Fatal(err)
	}
	svc.marketData = marketData
	status, body = request("/v1/market-data/candles?symbol=MSFT&interval=1d")
	if status != http.StatusNotFound || body != (APIError{Code: "market_data_not_found", Message: "market data was not found"}) {
		t.Fatalf("not found status=%d body=%+v", status, body)
	}
	svc.marketData = marketDataPortFunc(func(context.Context, string, string) (*MarketDataSeries, error) {
		return &MarketDataSeries{Symbol: "MSFT", Interval: "1d", Bars: []MarketDataBar{{At: "2026-01-07T00:00:00Z"}}}, nil
	})
	status, body = request("/v1/market-data/candles?symbol=AAPL&interval=1d")
	if status != http.StatusInternalServerError || body != (APIError{Code: "internal_error", Message: "internal server error"}) {
		t.Fatalf("mismatch status=%d body=%+v", status, body)
	}
	for _, adjustment := range []string{"", marketDataAdjustmentProviderAdjusted, "split_adjusted"} {
		svc.marketData = marketDataPortFunc(func(context.Context, string, string) (*MarketDataSeries, error) {
			return &MarketDataSeries{
				Symbol: "AAPL", Venue: "XNAS", Timezone: "America/New_York", Interval: "1d", PriceAdjustment: adjustment,
				Bars: []MarketDataBar{{At: "2026-01-07T00:00:00Z", Open: "10", High: "11", Low: "9", Close: "10", Volume: "1"}},
			}, nil
		})
		status, body = request("/v1/market-data/candles?symbol=AAPL&interval=1d")
		if status != http.StatusInternalServerError || body != (APIError{Code: "internal_error", Message: "internal server error"}) {
			t.Fatalf("adjustment=%q status=%d body=%+v", adjustment, status, body)
		}
	}
}

func TestMarketDataOpenAPIRequiresPriceAdjustment(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Enum  []string        `json:"enum"`
					Const json.RawMessage `json:"const"`
				} `json:"properties"`
				AllOf []struct {
					Properties map[string]struct {
						Const json.RawMessage `json:"const"`
					} `json:"properties"`
				} `json:"allOf"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	base := document.Components.Schemas["MarketDataCandles"]
	if !slices.Contains(base.Required, "price_adjustment") ||
		!slices.Equal(base.Properties["price_adjustment"].Enum, []string{marketDataAdjustmentUnspecified, marketDataAdjustmentProviderAdjusted}) {
		t.Fatal("MarketDataCandles must require the canonical price_adjustment enum")
	}
	for _, field := range []string{"instrument_id", "currency", "bar_time_basis"} {
		if !slices.Contains(base.Required, field) {
			t.Fatalf("MarketDataCandles must disclose %s", field)
		}
	}
	if string(base.Properties["bar_time_basis"].Const) != `"unspecified"` {
		t.Fatal("bar time semantics must remain explicit unknown")
	}
	fixture := document.Components.Schemas["LocalFixtureMarketDataCandles"]
	if len(fixture.AllOf) != 2 || string(fixture.AllOf[1].Properties["price_adjustment"].Const) != `"unspecified"` {
		t.Fatal("LocalFixtureMarketDataCandles must pin price_adjustment=unspecified")
	}
}

func TestMarketDataSourceIdentity(t *testing.T) {
	for _, name := range []string{"market-bars.csv", "market-bars-identified.csv"} {
		t.Run(name, func(t *testing.T) {
			svc, _ := testService(t, []time.Time{mustTime("2026-01-10T15:01:00Z")}, nil)
			port, err := svc.loadMarketDataFixture(fixturePath(name))
			if err != nil {
				t.Fatal(err)
			}
			svc.marketData = port
			w := httptest.NewRecorder()
			svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/market-data/candles?symbol=AAPL&interval=1d", nil))
			var got map[string]any
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
			if _, ok := got["instrument_id"]; !ok {
				t.Fatal("source identity was not disclosed")
			}
			if got["bar_time_basis"] != "unspecified" {
				t.Fatal("timestamp semantics must remain unknown")
			}
			if name == "market-bars.csv" {
				if got["instrument_id"] != nil || got["currency"] != nil {
					t.Fatal("legacy fixture identity was inferred")
				}
			} else if got["instrument_id"] != "instrument_aapl" || got["currency"] != "USD" {
				t.Fatal("explicit fixture identity was lost")
			}
		})
	}
}

func TestMarketDataPortInvalidIdentityIsNotExposed(t *testing.T) {
	usd, invalidID, id, invalidCurrency := "USD", "private bad id", "instrument_aapl", "usd"
	for _, fields := range [][2]*string{{nil, &usd}, {&invalidID, &usd}, {&id, &invalidCurrency}} {
		svc, _ := testService(t, nil, nil)
		svc.marketData = marketDataPortFunc(func(context.Context, string, string) (*MarketDataSeries, error) {
			return &MarketDataSeries{InstrumentID: fields[0], Currency: fields[1], Symbol: "AAPL", Interval: "1d", PriceAdjustment: marketDataAdjustmentUnspecified, Bars: []MarketDataBar{{At: "2026-01-07T00:00:00Z"}}}, nil
		})
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/market-data/candles?symbol=AAPL&interval=1d", nil))
		if w.Code != 500 || strings.Contains(w.Body.String(), "private bad id") {
			t.Fatalf("unsafe response: %d %s", w.Code, w.Body.String())
		}
	}
}
