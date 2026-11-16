package domain

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPortfolioBenchmarksIndependentReferenceCases(t *testing.T) {
	var cases struct {
		Cases []struct {
			Name          string  `json:"name"`
			Currency      string  `json:"currency"`
			Start         string  `json:"start"`
			End           string  `json:"end"`
			FXStart       *string `json:"fx_start"`
			FXEnd         *string `json:"fx_end"`
			PortfolioRate string  `json:"portfolio_rate"`
			NativeRate    *string `json:"native_rate"`
			KRWRate       *string `json:"krw_rate"`
			Difference    *string `json:"difference_percentage_points"`
		} `json:"cases"`
		Differences []struct {
			PortfolioRate string `json:"portfolio_rate"`
			BenchmarkRate string `json:"benchmark_rate"`
			Expected      string `json:"expected"`
		} `json:"differences"`
	}
	if err := json.Unmarshal(fixture(t, "portfolio-benchmark-cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases.Cases {
		t.Run(test.Name, func(t *testing.T) {
			input := BenchmarkSeriesInput{SchemaVersion: "benchmark-index-series.v1", BenchmarkID: "test_reference", Label: "Synthetic test reference", Currency: "KRW", ReturnBasis: "gross_total_return", SourceReference: "https://example.invalid/test-methodology", SourceVersion: "test-v1", Sample: true, FetchedAt: "2026-01-01T00:00:00Z", Points: []BenchmarkIndexPoint{{ObservedAt: "2025-01-01T00:00:00Z"}, {ObservedAt: "2026-01-01T00:00:00Z"}}}
			input.Currency = test.Currency
			input.Sample = false
			input.Points[0].Level = test.Start
			input.Points[1].Level = test.End
			record := BenchmarkVersion{Sequence: 1, Input: input, DataSHA256: strings.Repeat("1", 64), RecordedAt: "2026-01-01T00:00:00Z"}
			marks := twrMarks{}
			if test.FXStart != nil {
				marks.fx = []FXObservation{{ObservationID: "start", Source: "local_fixture", BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: *test.FXStart, ObservedAt: input.Points[0].ObservedAt, FetchedAt: input.FetchedAt, RecordedAt: record.RecordedAt}, {ObservationID: "end", Source: "local_fixture", BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: *test.FXEnd, ObservedAt: input.Points[1].ObservedAt, FetchedAt: input.FetchedAt, RecordedAt: record.RecordedAt}}
				var err error
				marks.fxTimes, err = currentFXObservationTimes(marks.fx)
				if err != nil {
					t.Fatal(err)
				}
			}
			portfolio := &PortfolioTWR{Status: "calculated", PeriodStart: stringPointer(input.Points[0].ObservedAt), PeriodEnd: "2026-01-01T00:00:00Z", CumulativeRate: &test.PortfolioRate}
			row, err := comparePortfolioBenchmark(record, portfolio, marks, mustTime(portfolio.PeriodEnd))
			if err != nil || !reflect.DeepEqual(row.Native.Rate, test.NativeRate) || !reflect.DeepEqual(row.KRW.Rate, test.KRWRate) || !reflect.DeepEqual(row.DifferencePercentagePoints, test.Difference) {
				t.Fatalf("independent rates differ: %+v %v", row, err)
			}
			if row.Sample != (test.Currency == "USD") {
				t.Fatal("FX sample provenance mismatch")
			}
		})
	}
	for _, test := range cases.Differences {
		got, err := BenchmarkDifference(test.PortfolioRate, test.BenchmarkRate)
		if err != nil || got == nil || *got != test.Expected {
			t.Fatalf("percentage-point bounds: %v %v", got, err)
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../../../../../contracts/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func mustTime(raw string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		panic(err)
	}
	return at
}
