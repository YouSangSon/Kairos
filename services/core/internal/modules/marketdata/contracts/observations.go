package contracts

import (
	"context"
	"time"

	storage "omni-folio/services/core/internal/storage/contracts"
)

type BenchmarkVersion struct {
	Sequence   int64
	Input      BenchmarkSeriesInput
	DataSHA256 string
	RecordedAt string
}

type ValuationInputs struct {
	Cutoff                time.Time
	FX                    []FXObservation
	Prices                []SecurityPriceObservation
	Benchmarks            []BenchmarkVersion
	BenchmarkVersionCount int
}

type ValuationQuery struct{ FX, Prices, Benchmarks bool }

type Queries interface {
	ReadValuationInputs(context.Context, storage.ObservationRef, ValuationQuery) (ValuationInputs, error)
}

type PriceIdentities struct {
	LedgerRevision string
	Identities     map[string]struct{}
}

type ScopedOwnerPriceImport interface {
	ImportOwnerPriceScoped(context.Context, storage.ObservationRef, OwnerPriceCommand, PriceIdentities) (*OwnerPriceSeriesReceipt, error)
}

type OwnerPriceCommand struct {
	Input      OwnerPriceSeriesInput
	Canonical  []byte
	DataSHA256 string
}

type OwnerPriceImports interface {
	ImportOwnerPrice(context.Context, []byte) (*OwnerPriceSeriesReceipt, error)
}

type AtomicOwnerPriceImport interface {
	ImportOwnerPriceAtomic(context.Context, OwnerPriceCommand) (*OwnerPriceSeriesReceipt, error)
}

type Imports interface {
	ImportOwnerFX(context.Context, []byte) (*OwnerFXSeriesReceipt, error)
	ImportBenchmark(context.Context, []byte) (*BenchmarkSeriesReceipt, error)
	BenchmarkStatus(context.Context, string, string) (*BenchmarkSeriesReceipt, error)
}

type Observations interface {
	RecordFX(context.Context, FXObservationInput) (*FXObservation, error)
	RecordPrice(context.Context, SecurityPriceObservationInput) (*SecurityPriceObservation, error)
	DeclareListing(context.Context, InstrumentListingInput) (*InstrumentListingEvent, error)
	RevokeListing(context.Context, InstrumentListingInput) (*InstrumentListingEvent, error)
	ResolveListing(context.Context, string, string, string) (*InstrumentListingEvent, error)
	LatestFX(context.Context, string, string, string, string) (*FXObservation, error)
}

type CandleSource interface {
	Candles(context.Context, string, string) (*MarketDataSeries, error)
}

type RecoveryProof struct {
	FXSHA256           string
	FXObservations     int
	PricesSHA256       string
	PriceObservations  int
	ListingsSHA256     string
	ListingEvents      int
	ActiveListings     int
	BenchmarkSHA256    string
	BenchmarkVersions  int
	BenchmarkPoints    int
	OwnerFXSHA256      string
	OwnerFXVersions    int
	OwnerFXPoints      int
	OwnerPriceSHA256   string
	OwnerPriceVersions int
	OwnerPricePoints   int
}

type BackupQuery struct{ Schema int }

type Recovery interface {
	VerifyBackup(context.Context, storage.ObservationRef, BackupQuery) (RecoveryProof, error)
	VerifyStartup(context.Context, storage.ObservationRef) error
}
