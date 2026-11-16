package codec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const ownerPriceTestInput = `{"schema_version":"owner-price-series.v1","source_version":"price-v1","instrument_id":"instrument_aapl","symbol":"AAPL","venue":"XNAS","currency":"USD","price_adjustment":"unadjusted","sample":false,"fetched_at":"2026-01-01T00:00:00Z","points":[{"observed_at":"2025-01-01T00:00:00Z","price":"100"},{"observed_at":"2026-01-01T00:00:00Z","price":"200"}]}`

func ownerPriceInputForTest(t testing.TB, change func(*OwnerPriceSeriesInput)) []byte {
	t.Helper()
	var input OwnerPriceSeriesInput
	if err := json.Unmarshal([]byte(ownerPriceTestInput), &input); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&input)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOwnerPriceClosedInputAndWholeVersionBounds(t *testing.T) {
	bad := map[string][]byte{
		"unknown":              []byte(strings.Replace(ownerPriceTestInput, `"sample":false`, `"sample":false,"provider_verified":true`, 1)),
		"escaped duplicate":    []byte(strings.Replace(ownerPriceTestInput, `"sample":false`, `"sample":false,"s\u0061mple":true`, 1)),
		"null":                 []byte(strings.Replace(ownerPriceTestInput, `"sample":false`, `"sample":null`, 1)),
		"null points":          ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Points = nil }),
		"phantom canonical id": ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.InstrumentID = "unrelated" }),
		"unknown point":        []byte(strings.Replace(ownerPriceTestInput, `"price":"200"`, `"price":"200","extra":true`, 1)),
		"adjusted":             ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.PriceAdjustment = "split_adjusted" }),
		"unspecified":          ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.PriceAdjustment = "unspecified" }),
		"zero":                 ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Points[0].Price = "0" }),
		"noncanonical":         ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Points[0].Price = "100.0" }),
		"duplicate time":       ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Points[1].ObservedAt = i.Points[0].ObservedAt }),
		"point after fetch":    ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Points[1].ObservedAt = "2026-01-01T00:00:00.000000001Z" }),
		"version bytes":        ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.SourceVersion = strings.Repeat("한", 43) }),
		"surrogate":            []byte(strings.Replace(ownerPriceTestInput, "price-v1", `\ud800`, 1)),
		"trailing":             []byte(ownerPriceTestInput + ` {}`),
		"raw bound":            []byte(strings.Repeat(" ", maxBodyBytes) + ownerPriceTestInput),
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := decodeOwnerPriceSeries(context.Background(), raw); err == nil {
				t.Fatal("invalid price input accepted")
			}
		})
	}
	for _, n := range []int{0, 10000, 10001} {
		raw := ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) {
			i.Points = []OwnerPricePoint{}
			for j := 0; j < n; j++ {
				i.Points = append(i.Points, OwnerPricePoint{ObservedAt: mustTime("2025-01-01T00:00:00Z").Add(time.Duration(j)).Format(time.RFC3339Nano), Price: "1"})
			}
		})
		i, _, _, err := decodeOwnerPriceSeries(context.Background(), raw)
		if n > 10000 {
			if err == nil {
				t.Fatal("point bound ignored")
			}
		} else if err != nil || len(i.Points) != n {
			t.Fatalf("valid count %d rejected: %v", n, err)
		}
	}
}

const ownerFXTestInput = `{"schema_version":"owner-fx-series.v1","source_version":"manual-v1","base_currency":"USD","quote_currency":"KRW","sample":false,"fetched_at":"2026-01-01T00:00:00Z","points":[{"observed_at":"2025-01-01T00:00:00Z","rate":"1000"},{"observed_at":"2026-01-01T00:00:00Z","rate":"900"}]}`

func ownerFXInputForTest(t testing.TB, change func(*OwnerFXSeriesInput)) []byte {
	t.Helper()
	var input OwnerFXSeriesInput
	if err := json.Unmarshal([]byte(ownerFXTestInput), &input); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&input)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOwnerFXClosedInputAndBounds(t *testing.T) {
	invalid := map[string][]byte{
		"unknown field":         []byte(strings.Replace(ownerFXTestInput, `"sample":false`, `"sample":false,"provider_verified":true`, 1)),
		"duplicate escaped key": []byte(strings.Replace(ownerFXTestInput, `"sample":false`, `"sample":false,"s\u0061mple":true`, 1)),
		"unknown point":         []byte(strings.Replace(ownerFXTestInput, `"rate":"900"`, `"rate":"900","extra":true`, 1)),
		"null sample":           []byte(strings.Replace(ownerFXTestInput, `"sample":false`, `"sample":null`, 1)),
		"null points":           ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Points = nil }),
		"same direction":        ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.BaseCurrency = "KRW" }),
		"lower direction":       ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.BaseCurrency = "usd" }),
		"zero":                  ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Points[0].Rate = "0" }),
		"numeric rate":          []byte(strings.Replace(ownerFXTestInput, `"rate":"900"`, `"rate":900`, 1)),
		"noncanonical rate":     ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Points[0].Rate = "1000.0" }),
		"duplicate point time":  ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Points[1].ObservedAt = i.Points[0].ObservedAt }),
		"point after fetch":     ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Points[1].ObservedAt = "2026-01-01T00:00:00.000000001Z" }),
		"version byte bound":    ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.SourceVersion = strings.Repeat("한", 43) }),
		"surrogate":             []byte(strings.Replace(ownerFXTestInput, `manual-v1`, `\ud800`, 1)),
		"trailing JSON":         []byte(ownerFXTestInput + ` {}`),
		"body bound":            []byte(strings.Repeat(" ", maxBodyBytes) + ownerFXTestInput),
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := DecodeOwnerFXSeries(context.Background(), raw); err == nil {
				t.Fatal("invalid owner declaration accepted")
			}
		})
	}
	for _, count := range []int{0, 10000, 10001} {
		raw := ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) {
			i.Points = []OwnerFXPoint{}
			for n := 0; n < count; n++ {
				i.Points = append(i.Points, OwnerFXPoint{ObservedAt: mustTime("2025-01-01T00:00:00Z").Add(time.Duration(n)).Format(time.RFC3339Nano), Rate: "1000"})
			}
		})
		input, canonical, _, err := DecodeOwnerFXSeries(context.Background(), raw)
		if count > 10000 {
			if err == nil {
				t.Fatal("point bound ignored")
			}
		} else if err != nil || len(input.Points) != count || count == 0 && !strings.Contains(string(canonical), `"points":[]`) {
			t.Fatalf("valid point boundary rejected: count=%d err=%v", count, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := DecodeOwnerFXSeries(ctx, []byte(ownerFXTestInput)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

const benchmarkSeriesTestInput = `{
 "schema_version":"benchmark-index-series.v1",
 "benchmark_id":"test_reference",
 "label":"Synthetic test reference",
 "currency":"KRW",
 "return_basis":"gross_total_return",
 "source_reference":"https://example.invalid/test-methodology",
 "source_version":"test-v1",
 "sample":true,
 "fetched_at":"2026-01-01T00:00:00Z",
 "points":[{"observed_at":"2025-01-01T00:00:00Z","level":"100"},{"observed_at":"2026-01-01T00:00:00Z","level":"110"}]
}`

func benchmarkInputForTest(t *testing.T, edit func(*BenchmarkSeriesInput)) []byte {
	t.Helper()
	var input BenchmarkSeriesInput
	if err := json.Unmarshal([]byte(benchmarkSeriesTestInput), &input); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&input)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestBenchmarkSeriesClosedInput(t *testing.T) {
	for _, currency := range []string{"KRW", "USD"} {
		for _, basis := range []string{"price_index", "gross_total_return", "net_total_return"} {
			raw := benchmarkInputForTest(t, func(i *BenchmarkSeriesInput) { i.Currency, i.ReturnBasis = currency, basis })
			input, canonical, sha, err := DecodeBenchmarkSeries(context.Background(), raw)
			if err != nil || input.Currency != currency || input.ReturnBasis != basis || len(canonical) == 0 || len(sha) != 64 {
				t.Fatalf("valid %s/%s rejected: %v", currency, basis, err)
			}
		}
	}
	replace := func(old, new string) string { return strings.Replace(benchmarkSeriesTestInput, old, new, 1) }
	cases := map[string]string{
		"unknown":               replace(`"sample":true`, `"extra":1,"sample":true`),
		"duplicate":             replace(`"sample":true`, `"sample":false,"sample":true`),
		"escaped duplicate":     replace(`"sample":true`, `"\u0073ample":false,"sample":true`),
		"missing":               replace(`"sample":true,`, ``),
		"null":                  replace(`"sample":true`, `"sample":null`),
		"wrong boolean":         replace(`"sample":true`, `"sample":"true"`),
		"unknown schema":        replace(benchmarkSeriesSchema, "benchmark-index-series.v2"),
		"currency":              replace(`"KRW"`, `"EUR"`),
		"basis":                 replace(`"gross_total_return"`, `"adjusted_close"`),
		"id whitespace":         replace(`"test_reference"`, `"test reference"`),
		"label whitespace":      replace(`Synthetic test reference`, ` Synthetic test reference`),
		"label control":         replace(`Synthetic test reference`, `Synthetic\nreference`),
		"source credentials":    replace(`https://example.invalid/test-methodology`, `https://secret:token@example.invalid/test`),
		"source query":          replace(`https://example.invalid/test-methodology`, `https://example.invalid/test?token=secret`),
		"source empty query":    replace(`https://example.invalid/test-methodology`, `https://example.invalid/test?`),
		"source fragment":       replace(`https://example.invalid/test-methodology`, `https://example.invalid/test#secret`),
		"source scheme":         replace(`https://example.invalid/test-methodology`, `file:///tmp/index.json`),
		"offset time":           replace(`2025-01-01T00:00:00Z`, `2025-01-01T09:00:00+09:00`),
		"noncanonical time":     replace(`2025-01-01T00:00:00Z`, `2025-01-01T00:00:00.0Z`),
		"future point":          replace(`2025-01-01T00:00:00Z`, `2027-01-01T00:00:00Z`),
		"duplicate time":        replace(`2025-01-01T00:00:00Z`, `2026-01-01T00:00:00Z`),
		"duplicate point field": replace(`"level":"100"`, `"level":"99","level":"100"`),
		"unknown point field":   replace(`"level":"100"`, `"level":"100","currency":"KRW"`),
		"null point":            replace(`"level":"100"`, `"level":null`),
		"number level":          replace(`"level":"100"`, `"level":100`),
		"negative level":        replace(`"level":"100"`, `"level":"-1"`),
		"noncanonical level":    replace(`"level":"100"`, `"level":"100.0"`),
		"exponent level":        replace(`"level":"100"`, `"level":"1e2"`),
		"unpaired surrogate":    replace(`Synthetic test reference`, `Synthetic\ud800reference`),
		"trailing":              benchmarkSeriesTestInput + `{}`,
		"invalid UTF8":          replace(`Synthetic test reference`, string([]byte{0xff})),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := DecodeBenchmarkSeries(context.Background(), []byte(raw)); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	for _, field := range []string{"schema_version", "benchmark_id", "label", "currency", "return_basis", "source_reference", "source_version", "sample", "fetched_at", "points"} {
		var object map[string]any
		if err := json.Unmarshal([]byte(benchmarkSeriesTestInput), &object); err != nil {
			t.Fatal(err)
		}
		delete(object, field)
		raw, _ := json.Marshal(object)
		if _, _, _, err := DecodeBenchmarkSeries(context.Background(), raw); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
}

func TestBenchmarkSeriesInputBoundsAndCancellation(t *testing.T) {
	for name, edit := range map[string]func(*BenchmarkSeriesInput){
		"no points":     func(i *BenchmarkSeriesInput) { i.Points = nil },
		"id bytes":      func(i *BenchmarkSeriesInput) { i.BenchmarkID = strings.Repeat("한", 43) },
		"label bytes":   func(i *BenchmarkSeriesInput) { i.Label = strings.Repeat("한", 54) },
		"version bytes": func(i *BenchmarkSeriesInput) { i.SourceVersion = strings.Repeat("v", 129) },
		"reference bytes": func(i *BenchmarkSeriesInput) {
			i.SourceReference = "https://example.invalid/" + strings.Repeat("x", 512)
		},
		"decimal bytes": func(i *BenchmarkSeriesInput) { i.Points[0].Level = strings.Repeat("9", 65) },
		"zero timestamp duplicate": func(i *BenchmarkSeriesInput) {
			i.Points[0].ObservedAt, i.Points[1].ObservedAt = "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z"
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := DecodeBenchmarkSeries(context.Background(), benchmarkInputForTest(t, edit)); err == nil {
				t.Fatal("bound exceeded")
			}
		})
	}
	for _, count := range []int{10000, 10001} {
		raw := benchmarkInputForTest(t, func(i *BenchmarkSeriesInput) {
			i.Points = make([]BenchmarkIndexPoint, count)
			for n := range i.Points {
				i.Points[n] = BenchmarkIndexPoint{ObservedAt: time.Date(2025, 1, 1, 0, 0, n, 0, time.UTC).Format(time.RFC3339Nano), Level: "0"}
			}
		})
		if _, _, _, err := DecodeBenchmarkSeries(context.Background(), raw); (err == nil) != (count == 10000) {
			t.Fatalf("point bound %d: %v", count, err)
		}
	}
	oversized := append(bytes.Repeat([]byte(" "), maxBodyBytes), []byte(benchmarkSeriesTestInput)...)
	if _, _, _, err := DecodeBenchmarkSeries(context.Background(), oversized); err == nil {
		t.Fatal("oversized source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := DecodeBenchmarkSeries(ctx, []byte(benchmarkSeriesTestInput)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
func mustTime(raw string) time.Time {
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		panic(err)
	}
	return v
}
func decodeOwnerPriceSeries(ctx context.Context, raw []byte) (*OwnerPriceSeriesInput, []byte, string, error) {
	return DecodeOwnerPriceSeries(ctx, raw, func(symbol string) string { return "instrument_" + strings.ToLower(symbol) })
}

func TestOwnerPriceCanonicalIdentityCallbackKeepsValidationStage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*OwnerPriceSeriesInput)
		wantCalls int
		valid     bool
	}{
		{name: "valid", wantCalls: 1, valid: true},
		{name: "invalid identifier", change: func(i *OwnerPriceSeriesInput) { i.SourceVersion = "" }},
		{name: "lowercase symbol", change: func(i *OwnerPriceSeriesInput) { i.Symbol = "aapl" }},
		{name: "wrong canonical identity", change: func(i *OwnerPriceSeriesInput) { i.InstrumentID = "other" }, wantCalls: 1},
		{name: "invalid currency", change: func(i *OwnerPriceSeriesInput) { i.Currency = "usd" }, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, _, _, err := DecodeOwnerPriceSeries(context.Background(), ownerPriceInputForTest(t, tc.change), func(symbol string) string {
				calls++
				if symbol != "AAPL" {
					t.Fatalf("callback symbol = %s", symbol)
				}
				return "instrument_aapl"
			})
			if calls != tc.wantCalls || (err == nil) != tc.valid {
				t.Fatalf("callback calls=%d, err=%v", calls, err)
			}
		})
	}
}
