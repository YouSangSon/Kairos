package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

func TestBenchmarkSeriesCLIImportsAnExplicitIndexVersion(t *testing.T) {
	svc, path := testService(t, nil, nil)
	input := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(input, []byte(benchmarkSeriesTestInput), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runContext(context.Background(), []string{"benchmark-import", "-db", path, "-input", input}); err != nil {
		t.Fatalf("versioned benchmark admission unavailable: %v", err)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM benchmark_series_versions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one immutable version, got %d", count)
	}
}

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

func importBenchmarkForTest(t *testing.T, svc *Service, raw []byte) *BenchmarkSeriesReceipt {
	t.Helper()
	receipt, err := svc.importBenchmarkSeries(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
