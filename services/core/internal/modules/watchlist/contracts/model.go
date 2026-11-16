package contracts

import (
	"context"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"time"
)

type WatchlistEntry struct {
	Venue    string `json:"venue"`
	Symbol   string `json:"symbol"`
	Currency string `json:"currency"`
}

type WatchlistState struct {
	Revision  int64            `json:"revision"`
	UpdatedAt *string          `json:"updated_at"`
	Entries   []WatchlistEntry `json:"entries"`
}

type WatchlistChange struct {
	ExpectedRevision int64            `json:"expected_revision"`
	Entries          []WatchlistEntry `json:"entries"`
}

type WatchlistPrice struct {
	Source          string  `json:"source"`
	SourceVersion   *string `json:"source_version"`
	Price           string  `json:"price"`
	PriceAdjustment string  `json:"price_adjustment"`
	ObservedAt      string  `json:"observed_at"`
	FetchedAt       string  `json:"fetched_at"`
	RecordedAt      string  `json:"recorded_at"`
	Sample          bool    `json:"sample"`
}

type WatchlistQuote struct {
	WatchlistEntry
	Status string          `json:"status"`
	Price  *WatchlistPrice `json:"price"`
}

type WatchlistReport struct {
	PolicyVersion            string           `json:"policy_version"`
	MaxObservationAgeSeconds int              `json:"max_observation_age_seconds"`
	ObservedAt               string           `json:"observed_at"`
	Watchlist                WatchlistState   `json:"watchlist"`
	Quotes                   []WatchlistQuote `json:"quotes"`
}
type RecoveryProof struct {
	SHA256   string
	Revision int64
	Entries  int
}
type Inputs struct {
	Cutoff time.Time
	State  WatchlistState
	Prices []market.SecurityPriceObservation
}
type InputsReader interface {
	ReadWatchlistInputs(context.Context) (Inputs, error)
}
type StateReader interface {
	ReadState(context.Context, storage.ObservationRef) (WatchlistState, error)
}
type Recovery interface {
	VerifyBackup(context.Context, storage.ObservationRef) (RecoveryProof, error)
}
type Store interface {
	Replace(context.Context, WatchlistChange) (*WatchlistState, error)
}
type API interface {
	Read(context.Context) (*WatchlistReport, error)
	Replace(context.Context, WatchlistChange) (*WatchlistState, error)
}
