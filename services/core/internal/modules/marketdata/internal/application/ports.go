package application

import (
	"context"
	"time"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
)

type SeriesProof struct {
	SHA256           string
	Versions, Points int
}
type ObservationProof struct {
	SHA256       string
	Observations int
}
type ListingProof struct {
	SHA256         string
	Events, Active int
}
type OwnerFXRecord struct {
	Sequence               int64
	Input                  c.OwnerFXSeriesInput
	DataSHA256, RecordedAt string
}
type OwnerPriceRecord struct {
	Sequence               int64
	Input                  c.OwnerPriceSeriesInput
	DataSHA256, RecordedAt string
}
type BenchmarkRecord struct {
	Sequence               int64
	Input                  c.BenchmarkSeriesInput
	DataSHA256, RecordedAt string
}

type Reader interface {
	Schema(context.Context) error
	FXBySource(context.Context, string, string) (*c.FXObservation, error)
	PriceBySource(context.Context, string, string) (*c.SecurityPriceObservation, error)
	LatestFX(context.Context, string, string, string, string) (*c.FXObservation, error)
	FXObservations(context.Context) ([]c.FXObservation, ObservationProof, error)
	PriceObservations(context.Context) ([]c.SecurityPriceObservation, ObservationProof, error)
	Listings(context.Context) (map[string]c.InstrumentListingEvent, ListingProof, error)
	ResolveListing(context.Context, string, string, string) (*c.InstrumentListingEvent, error)
	PreviousListing(context.Context, string, string, string) (string, error)
	OwnerFX(context.Context, string, string, string) (*OwnerFXRecord, SeriesProof, error)
	OwnerPrice(context.Context, map[string]struct{}, string, string, string) (*OwnerPriceRecord, SeriesProof, error)
	Benchmark(context.Context, string, string) (*BenchmarkRecord, SeriesProof, error)
	LatestBenchmark(context.Context, string) (*c.BenchmarkSeriesInput, error)
	LastRecorded(context.Context, string) (string, error)
}

type Scope interface {
	Reader
	InsertFX(context.Context, c.FXObservation) error
	InsertPrice(context.Context, c.SecurityPriceObservation) error
	InsertListing(context.Context, c.InstrumentListingEvent) error
	InsertOwnerFX(context.Context, OwnerFXRecord, []byte) error
	InsertOwnerPrice(context.Context, OwnerPriceRecord, []byte) error
	InsertBenchmark(context.Context, BenchmarkRecord, []byte) error
}

type Repository interface {
	Write(context.Context, func(Scope) error) error
	Read(context.Context, func(Reader) error) error
	ReadInputs(context.Context, storage.ObservationRef, map[string]struct{}, c.ValuationQuery) (c.ValuationInputs, error)
	HasOwnerPrices(context.Context, storage.ObservationRef) (bool, error)
	Verify(context.Context, storage.ObservationRef, map[string]struct{}, c.BackupQuery) (c.RecoveryProof, error)
	ProveBenchmark(context.Context, storage.ObservationRef) error
	ProveOwnerFX(context.Context, storage.ObservationRef) error
	ProveOwnerPrice(context.Context, storage.ObservationRef, map[string]struct{}) error
}

type Decoder interface {
	OwnerFX(context.Context, []byte) (*c.OwnerFXSeriesInput, []byte, string, error)
	OwnerPrice(context.Context, []byte) (*c.OwnerPriceSeriesInput, []byte, string, error)
	Benchmark(context.Context, []byte) (*c.BenchmarkSeriesInput, []byte, string, error)
	FX(c.FXObservationInput) error
	Price(c.SecurityPriceObservationInput) error
	Listing(c.InstrumentListingInput) error
}

type Ledger interface {
	AdmittedPriceIdentities(context.Context, storage.ObservationRef) (c.PriceIdentities, error)
}

type Service struct {
	store  Repository
	decode Decoder
	ledger Ledger
	now    func() time.Time
	id     func(string) string
}

func New(store Repository, decode Decoder, ledger Ledger, now func() time.Time, id func(string) string) *Service {
	return &Service{store, decode, ledger, now, id}
}

type ScopedPriceScope interface {
	Schema(context.Context) error
	PriceObservations(context.Context) ([]c.SecurityPriceObservation, ObservationProof, error)
	OwnerPrice(context.Context, map[string]struct{}, string, string, string) (*OwnerPriceRecord, SeriesProof, error)
	LastRecorded(context.Context, string) (string, error)
	InsertOwnerPrice(context.Context, OwnerPriceRecord, []byte) error
}
type ScopedStore interface {
	ScopedImport(context.Context, storage.ObservationRef, func(ScopedPriceScope) error) error
}
type ScopedImporter struct {
	store ScopedStore
	now   func() time.Time
}

func NewScopedImporter(store ScopedStore, now func() time.Time) *ScopedImporter {
	return &ScopedImporter{store, now}
}
