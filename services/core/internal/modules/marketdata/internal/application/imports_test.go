package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
)

type importScope struct {
	Scope
	calls    *[]string
	priorSHA string
}

func (s importScope) mark(name string)             { *s.calls = append(*s.calls, name) }
func (s importScope) Schema(context.Context) error { s.mark("schema"); return nil }
func (s importScope) FXObservations(context.Context) ([]c.FXObservation, ObservationProof, error) {
	s.mark("legacy")
	return nil, ObservationProof{}, nil
}
func (s importScope) PriceObservations(context.Context) ([]c.SecurityPriceObservation, ObservationProof, error) {
	s.mark("legacy")
	return nil, ObservationProof{}, nil
}
func (s importScope) OwnerFX(context.Context, string, string, string) (*OwnerFXRecord, SeriesProof, error) {
	s.mark("prior")
	if s.priorSHA != "" {
		return &OwnerFXRecord{DataSHA256: s.priorSHA}, SeriesProof{}, nil
	}
	return nil, SeriesProof{}, nil
}
func (s importScope) OwnerPrice(context.Context, map[string]struct{}, string, string, string) (*OwnerPriceRecord, SeriesProof, error) {
	s.mark("prior")
	if s.priorSHA != "" {
		return &OwnerPriceRecord{DataSHA256: s.priorSHA}, SeriesProof{}, nil
	}
	return nil, SeriesProof{}, nil
}
func (s importScope) Benchmark(context.Context, string, string) (*BenchmarkRecord, SeriesProof, error) {
	s.mark("prior")
	if s.priorSHA != "" {
		return &BenchmarkRecord{Input: c.BenchmarkSeriesInput{Points: []c.BenchmarkIndexPoint{{ObservedAt: "2026-01-01T00:00:00Z", Level: "1"}}}, DataSHA256: s.priorSHA}, SeriesProof{}, nil
	}
	return nil, SeriesProof{}, nil
}
func (s importScope) LatestBenchmark(context.Context, string) (*c.BenchmarkSeriesInput, error) {
	s.mark("definition")
	return nil, nil
}
func (s importScope) LastRecorded(context.Context, string) (string, error) {
	s.mark("last")
	return "", nil
}
func (s importScope) InsertOwnerFX(context.Context, OwnerFXRecord, []byte) error {
	s.mark("insert")
	return nil
}
func (s importScope) InsertOwnerPrice(context.Context, OwnerPriceRecord, []byte) error {
	s.mark("insert")
	return nil
}
func (s importScope) InsertBenchmark(context.Context, BenchmarkRecord, []byte) error {
	s.mark("insert")
	return nil
}

type importStore struct {
	Repository
	scope importScope
}

func (s importStore) Write(_ context.Context, action func(Scope) error) error {
	s.scope.mark("write")
	return action(s.scope)
}
func (s importStore) ScopedImport(_ context.Context, _ storage.ObservationRef, action func(ScopedPriceScope) error) error {
	s.scope.mark("scoped")
	return action(s.scope)
}

type importDecoder struct {
	Decoder
	calls *[]string
}

func (d importDecoder) OwnerFX(context.Context, []byte) (*c.OwnerFXSeriesInput, []byte, string, error) {
	*d.calls = append(*d.calls, "decode")
	return &c.OwnerFXSeriesInput{FetchedAt: "2026-01-01T00:00:00Z"}, nil, "incoming", nil
}
func (d importDecoder) Benchmark(context.Context, []byte) (*c.BenchmarkSeriesInput, []byte, string, error) {
	*d.calls = append(*d.calls, "decode")
	return &c.BenchmarkSeriesInput{FetchedAt: "2026-01-01T00:00:00Z", Points: []c.BenchmarkIndexPoint{{ObservedAt: "2026-01-01T00:00:00Z", Level: "1"}}}, nil, "incoming", nil
}

func TestImportPoliciesPreserveReadClockWriteOrder(t *testing.T) {
	for _, kind := range []string{"fx", "benchmark", "price"} {
		for _, version := range []string{"new", "duplicate", "conflict"} {
			t.Run(kind+"/"+version, func(t *testing.T) {
				calls := []string{}
				scope := importScope{calls: &calls}
				if version == "duplicate" {
					scope.priorSHA = "incoming"
				}
				if version == "conflict" {
					scope.priorSHA = "different"
				}
				store := importStore{scope: scope}
				clockCalls := 0
				now := func() time.Time {
					clockCalls++
					calls = append(calls, "clock")
					return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
				}
				service := New(store, importDecoder{calls: &calls}, nil, now, nil)
				var err error
				switch kind {
				case "fx":
					_, err = service.ImportOwnerFX(context.Background(), nil)
				case "benchmark":
					_, err = service.ImportBenchmark(context.Background(), nil)
				case "price":
					_, err = NewScopedImporter(store, now).ImportOwnerPriceScoped(context.Background(), storage.ObservationRef{}, c.OwnerPriceCommand{Input: c.OwnerPriceSeriesInput{InstrumentID: "instrument", Currency: "USD", FetchedAt: "2026-01-01T00:00:00Z"}, DataSHA256: "incoming"}, c.PriceIdentities{Identities: map[string]struct{}{"instrument\x00USD": {}}})
				}
				if version == "conflict" {
					if !errors.Is(err, c.ErrConflict) {
						t.Fatalf("error = %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				want := []string{"decode", "write", "schema"}
				if kind == "price" {
					want = []string{"scoped", "schema"}
				}
				if kind != "benchmark" {
					want = append(want, "legacy")
				}
				want = append(want, "prior")
				wantClocks := 0
				if version == "new" {
					if kind == "benchmark" {
						want = append(want, "definition")
					}
					want = append(want, "last", "clock", "insert")
					wantClocks = 1
				}
				if !reflect.DeepEqual(calls, want) || clockCalls != wantClocks {
					t.Fatalf("calls = %v, clocks=%d; want %v, clocks=%d", calls, clockCalls, want, wantClocks)
				}
			})
		}
	}
}
