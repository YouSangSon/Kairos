package domain

import (
	"context"
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"sort"
	"strings"
	"time"
)

const portfolioBenchmarksPolicy = "retrospective_latest_index_24h_native_and_krw_v1"
const portfolioBenchmarksOwnerFXPolicy = "retrospective_latest_index_24h_native_and_krw_v2"

// shortcut: compare up to32 latest IDs; add explicit selection or paging when owners need more.
const maxPortfolioBenchmarks = 32

func publicBenchmarkFX(fx *FXObservation) *BenchmarkFXMark {
	mark := &BenchmarkFXMark{ObservedAt: fx.ObservedAt, Rate: fx.Rate}
	if fx.Source == ownerFXSource {
		mark.Source, mark.SourceVersion, mark.Sample = fx.Source, fx.OwnerVersion, &fx.OwnerSample
	}
	return mark
}

func latestBenchmarkPoint(points []BenchmarkIndexPoint, at time.Time) *BenchmarkIndexPoint {
	key := strings.TrimSuffix(at.UTC().Format(time.RFC3339Nano), "Z")
	index := sort.Search(len(points), func(i int) bool {
		return strings.TrimSuffix(points[i].ObservedAt, "Z") > key
	})
	if index == 0 {
		return nil
	}
	point := points[index-1]
	return &point
}

func comparePortfolioBenchmark(record benchmarkSeriesRecord, portfolio *PortfolioTWR, marks twrMarks, asOf time.Time) (PortfolioBenchmark, error) {
	row := PortfolioBenchmark{Series: benchmarkReceipt(&record), Sample: record.Input.Sample, Native: UnavailableBenchmarkRate("portfolio_unavailable"), KRW: UnavailableBenchmarkRate("portfolio_unavailable")}
	if portfolio.Status != "calculated" {
		return row, nil
	}
	if portfolio.PeriodStart == nil || portfolio.CumulativeRate == nil {
		return row, errors.New("calculated TWR omitted its comparison inputs")
	}
	start, err := time.Parse(time.RFC3339Nano, *portfolio.PeriodStart)
	if err != nil {
		return row, err
	}
	row.Start = latestBenchmarkPoint(record.Input.Points, start)
	row.End = latestBenchmarkPoint(record.Input.Points, asOf)
	issue := ""
	if row.Start == nil || row.End == nil {
		issue = "missing_benchmark_point"
	} else {
		startObserved, _ := time.Parse(time.RFC3339Nano, row.Start.ObservedAt)
		endObserved, _ := time.Parse(time.RFC3339Nano, row.End.ObservedAt)
		if start.Sub(startObserved) > cashFXMaxAge || asOf.Sub(endObserved) > cashFXMaxAge {
			issue = "stale_benchmark_point"
		} else if row.Start.Level == "0" {
			issue = "zero_benchmark_start"
		}
	}
	if issue != "" {
		row.Native, row.KRW = UnavailableBenchmarkRate(issue), UnavailableBenchmarkRate(issue)
		return row, nil
	}
	startLevel, err := exact.ParseDecimal(row.Start.Level)
	if err != nil {
		return row, err
	}
	endLevel, err := exact.ParseDecimal(row.End.Level)
	if err != nil {
		return row, err
	}
	row.Native = BenchmarkRate(new(big.Rat).Quo(endLevel, startLevel))
	if record.Input.Currency == "KRW" {
		row.KRW = row.Native
	} else {
		startFX := historicalXIRRFX(marks.fx, marks.fxTimes, record.Input.Currency, start, asOf)
		endFX := historicalXIRRFX(marks.fx, marks.fxTimes, record.Input.Currency, asOf, asOf)
		if startFX != nil {
			row.StartFX = publicBenchmarkFX(startFX)
			row.Sample = row.Sample || fxIsSample(startFX)
		}
		if endFX != nil {
			row.EndFX = publicBenchmarkFX(endFX)
			row.Sample = row.Sample || fxIsSample(endFX)
		}
		if startFX == nil || endFX == nil {
			row.KRW = UnavailableBenchmarkRate("missing_historical_fx")
		} else if start.Sub(marks.fxTimes[fxTimeKey(*startFX)][0]) > cashFXMaxAge || asOf.Sub(marks.fxTimes[fxTimeKey(*endFX)][0]) > cashFXMaxAge {
			row.KRW = UnavailableBenchmarkRate("stale_historical_fx")
		} else {
			startRate, err := exact.ParseDecimal(startFX.Rate)
			if err != nil {
				return row, err
			}
			endRate, err := exact.ParseDecimal(endFX.Rate)
			if err != nil {
				return row, err
			}
			// Convert each endpoint before taking the ratio; never convert a rounded native rate.
			row.KRW = BenchmarkRate(new(big.Rat).Quo(new(big.Rat).Mul(endLevel, endRate), new(big.Rat).Mul(startLevel, startRate)))
		}
	}
	if row.KRW.Rate != nil {
		row.DifferencePercentagePoints, err = BenchmarkDifference(*portfolio.CumulativeRate, *row.KRW.Rate)
		if err != nil {
			return row, err
		}
	}
	return row, nil
}

func Benchmarks(ctx context.Context, all Inputs) (*PortfolioBenchmarks, error) {
	asOf := all.Cutoff
	inputs, err := fxInputs(all)
	if err != nil {
		return nil, err
	}
	portfolio, appErr := TWR(ctx, all, nil)
	if appErr != nil {
		return nil, appErr
	}
	marks := twrMarks{fx: inputs.observations, fxTimes: inputs.times}

	r := &PortfolioBenchmarks{PolicyVersion: portfolioBenchmarksPolicy, MaxObservationAgeSeconds: int64(cashFXMaxAge / time.Second), Portfolio: portfolio, Benchmarks: []PortfolioBenchmark{}, Issues: []APIError{}}
	for _, record := range all.Market.Benchmarks {
		if len(r.Benchmarks) == maxPortfolioBenchmarks {
			r.Benchmarks = []PortfolioBenchmark{}
			r.Issues = append(r.Issues, APIError{Code: "benchmark_limit", Message: "benchmark comparison exceeds the declared index count limit"})
			break
		}
		row, err := comparePortfolioBenchmark(record, portfolio, marks, asOf)
		if err != nil {
			return nil, err
		}
		r.Benchmarks = append(r.Benchmarks, row)
	}

	if len(r.Benchmarks) == 0 && len(r.Issues) == 0 {
		code := "no_benchmarks"
		if all.Market.BenchmarkVersionCount != 0 {
			code = "no_known_benchmarks"
		}
		r.Issues = append(r.Issues, APIError{Code: code, Message: "no benchmark version is available at the report cutoff"})
	}
	if usesOwnerFX(portfolio.Issues) {
		r.PolicyVersion = portfolioBenchmarksOwnerFXPolicy
	}
	for _, row := range r.Benchmarks {
		if row.StartFX != nil && row.StartFX.Source == ownerFXSource || row.EndFX != nil && row.EndFX.Source == ownerFXSource {
			r.PolicyVersion = portfolioBenchmarksOwnerFXPolicy
		}
	}
	if usesOwnerPrice(portfolio.Issues) {
		r.PolicyVersion = "retrospective_latest_index_24h_native_and_krw_v3"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}
