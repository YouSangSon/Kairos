package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	storagepool "omni-folio/services/core/internal/storage/sqlite"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
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

func importBenchmarkForTest(t *testing.T, svc *conservationFixture, raw []byte) *BenchmarkSeriesReceipt {
	t.Helper()
	receipt, err := svc.ImportBenchmark(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestBenchmarkSeriesVersionsAreImmutableAndRetryDoesNotPromote(t *testing.T) {
	svc, _ := conservationService(t, nil, nil)
	first := importBenchmarkForTest(t, svc, []byte(benchmarkSeriesTestInput))
	if first.StorageCheck != "verified" || first.Authority != "owner_declared" || !first.Sample || first.PointCount != 2 {
		t.Fatalf("misleading receipt: %+v", first)
	}
	secondRaw := benchmarkInputForTest(t, func(i *BenchmarkSeriesInput) { i.SourceVersion = "test-v2"; i.Points[1].Level = "120" })
	second := importBenchmarkForTest(t, svc, secondRaw)
	svc.now = func() time.Time { t.Fatal("exact retry read the clock"); return time.Time{} }
	retry := importBenchmarkForTest(t, svc, benchmarkInputForTest(t, nil))
	if !reflect.DeepEqual(first, retry) {
		t.Fatal("canonical retry changed original receipt")
	}
	latest, err := svc.BenchmarkStatus(context.Background(), first.BenchmarkID, "")
	if err != nil || !reflect.DeepEqual(second, latest) {
		t.Fatalf("old retry promoted past version: %v", err)
	}
	for name, edit := range map[string]func(*BenchmarkSeriesInput){
		"same version new data":    func(i *BenchmarkSeriesInput) { i.Points[1].Level = "999" },
		"same version new capture": func(i *BenchmarkSeriesInput) { i.FetchedAt = "2026-01-02T00:00:00Z" },
		"currency":                 func(i *BenchmarkSeriesInput) { i.SourceVersion, i.Currency = "test-v3", "USD" },
		"basis":                    func(i *BenchmarkSeriesInput) { i.SourceVersion, i.ReturnBasis = "test-v3", "price_index" },
		"reference": func(i *BenchmarkSeriesInput) {
			i.SourceVersion, i.SourceReference = "test-v3", "https://other.invalid/index"
		},
		"sample": func(i *BenchmarkSeriesInput) { i.SourceVersion, i.Sample = "test-v3", false },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.ImportBenchmark(context.Background(), benchmarkInputForTest(t, edit)); err == nil {
				t.Fatal("conflicting version admitted")
			}
		})
	}
	if _, err := svc.db.Exec(`UPDATE benchmark_series_versions SET source_version='changed'`); err == nil {
		t.Fatal("version updated")
	}
	if _, err := svc.db.Exec(`DELETE FROM benchmark_series_versions`); err == nil {
		t.Fatal("version deleted")
	}
	if _, err := svc.BenchmarkStatus(context.Background(), first.BenchmarkID, "absent"); !errors.Is(err, errBenchmarkSeriesNotFound) {
		t.Fatalf("missing version: %v", err)
	}
}

func TestBenchmarkSeriesConcurrentImportIsAtomic(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "same", true: "conflict"}[conflict], func(t *testing.T) {
			svc, path := conservationService(t, nil, nil)
			other, err := storagepool.OpenReadWriteExisting(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			second := conservationApplication(other, svc.ledger, func() time.Time { return priceMustTime("2026-01-10T15:01:00Z") }, svc.id)
			inputs := [][]byte{[]byte(benchmarkSeriesTestInput), benchmarkInputForTest(t, func(i *BenchmarkSeriesInput) {
				if conflict {
					i.Points[1].Level = "111"
				}
			})}
			start := make(chan struct{})
			outcomes := make(chan error, 2)
			var wg sync.WaitGroup
			for n, service := range []*application.Service{svc.Service, second} {
				wg.Add(1)
				go func(s *application.Service, raw []byte) {
					defer wg.Done()
					<-start
					_, err := s.ImportBenchmark(context.Background(), raw)
					outcomes <- err
				}(service, inputs[n])
			}
			close(start)
			wg.Wait()
			close(outcomes)
			failures := 0
			for err := range outcomes {
				if err != nil {
					failures++
				}
			}
			want := 0
			if conflict {
				want = 1
			}
			if failures != want {
				t.Fatalf("unexpected race outcomes: failures=%d", failures)
			}
			proof, err := proveBenchmarkSeriesRecovery(context.Background(), svc.reader())
			if err != nil || proof.Versions != 1 || proof.Points != 2 {
				t.Fatalf("duplicate/partial race write: %+v %v", proof, err)
			}
		})
	}
}

func TestBenchmarkSeriesSharedFixturesAndContract(t *testing.T) {
	schema := readJSONMap(t, filepath.Join(conservationContracts(), "benchmark-index-series.schema.json"))
	var fields map[string]any
	if err := json.Unmarshal(benchmarkInputForTest(t, nil), &fields); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jsonKeySet(fields), jsonStringSet(schema["required"])) {
		t.Fatal("input contract and runtime fields differ")
	}
	for _, currency := range []string{"KRW", "USD"} {
		input, _, _, err := decodeBenchmarkSeries(context.Background(), fixture(t, "benchmark-"+strings.ToLower(currency)+"-sample.json"))
		if err != nil || input.Currency != currency || !input.Sample || input.ReturnBasis != "price_index" || len(input.Points) != 2 {
			t.Fatalf("shared %s fixture drift: %v", currency, err)
		}
	}
}

func TestBenchmarkSeriesRejectsFutureAndBackwardsRecordClock(t *testing.T) {
	svc, _ := conservationService(t, []time.Time{priceMustTime("2026-01-10T00:00:00.1Z"), priceMustTime("2026-01-10T00:00:00Z")}, nil)
	importBenchmarkForTest(t, svc, []byte(benchmarkSeriesTestInput))
	if _, err := svc.ImportBenchmark(context.Background(), benchmarkInputForTest(t, func(i *BenchmarkSeriesInput) { i.SourceVersion = "test-v2" })); err == nil {
		t.Fatal("backwards record clock accepted")
	}
	if _, err := svc.ImportBenchmark(context.Background(), benchmarkInputForTest(t, func(i *BenchmarkSeriesInput) { i.SourceVersion = "test-v3"; i.FetchedAt = "2027-01-01T00:00:00Z" })); err == nil {
		t.Fatal("future capture accepted")
	}
	proof, err := proveBenchmarkSeriesRecovery(context.Background(), svc.reader())
	if err != nil || proof.Versions != 1 {
		t.Fatalf("rejected write changed journal: %+v %v", proof, err)
	}
}

func TestBenchmarkSeriesPrivateRecoveryRejectsPhysicalAndJournalTampering(t *testing.T) {
	for _, weakness := range []string{"strict", "unique"} {
		t.Run(weakness, func(t *testing.T) {
			svc, _ := conservationService(t, nil, nil)
			script, err := migrationFiles.ReadFile("migrations/023_benchmark_series_versions.sql")
			if err != nil {
				t.Fatal(err)
			}
			altered := string(script)
			if weakness == "strict" {
				altered = strings.Replace(altered, ") STRICT;", ");", 1)
			} else {
				altered = strings.Replace(altered, ",\n    UNIQUE (benchmark_id, source_version)", "", 1)
			}
			if altered == string(script) {
				t.Fatal("tamper fixture did not change DDL")
			}
			if _, err := svc.db.Exec(`DROP TABLE benchmark_series_versions`); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.db.Exec(altered); err != nil {
				t.Fatal(err)
			}
			if _, err := proveBenchmarkSeriesRecovery(context.Background(), svc.reader()); err == nil {
				t.Fatal("weakened empty table accepted")
			}
		})
	}
	for _, populated := range []bool{false, true} {
		for _, object := range []string{"benchmark_series_latest_idx", "benchmark_series_versions_no_update", "benchmark_series_versions_no_delete", "benchmark_series_versions_insert_guard"} {
			t.Run(object+map[bool]string{false: "empty", true: "populated"}[populated], func(t *testing.T) {
				svc, _ := conservationService(t, nil, nil)
				if populated {
					importBenchmarkForTest(t, svc, []byte(benchmarkSeriesTestInput))
				}
				kind := "TRIGGER"
				if object == "benchmark_series_latest_idx" {
					kind = "INDEX"
				}
				if _, err := svc.db.Exec("DROP " + kind + " " + object); err != nil {
					t.Fatal(err)
				}
				if _, err := proveBenchmarkSeriesRecovery(context.Background(), svc.reader()); err == nil {
					t.Fatal("weakened physical schema accepted")
				}
			})
		}
	}
	for _, column := range []string{"series_sha256", "record_sha256", "series_json"} {
		t.Run(column, func(t *testing.T) {
			svc, _ := conservationService(t, nil, nil)
			importBenchmarkForTest(t, svc, []byte(benchmarkSeriesTestInput))
			if _, err := svc.db.Exec(`DROP TRIGGER benchmark_series_versions_no_update`); err != nil {
				t.Fatal(err)
			}
			value := "'" + strings.Repeat("0", 64) + "'"
			if column == "series_json" {
				value = `json_set(series_json,'$.points[1].level','999')`
			}
			if _, err := svc.db.Exec("UPDATE benchmark_series_versions SET " + column + "=" + value); err != nil {
				t.Fatal(err)
			}
			script, err := migrationFiles.ReadFile("migrations/023_benchmark_series_versions.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, block := range strings.Split(string(script), "\n\n") {
				if strings.HasPrefix(block, "CREATE TRIGGER benchmark_series_versions_no_update\n") {
					if _, err := svc.db.Exec(block); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := requireBenchmarkSeriesSchema(context.Background(), svc.reader()); err != nil {
				t.Fatal(err)
			}
			if _, err := proveBenchmarkSeriesRecovery(context.Background(), svc.reader()); err == nil {
				t.Fatal("tampered journal accepted")
			}
			if _, err := svc.BenchmarkStatus(context.Background(), "test_reference", ""); err == nil {
				t.Fatal("tampered status published")
			}
		})
	}
}
