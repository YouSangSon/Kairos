package domain

import (
	"errors"
	"time"
)

type WatchlistEntry struct {
	Venue    string
	Symbol   string
	Currency string
}
type WatchlistState struct {
	Revision  int64
	UpdatedAt *string
	Entries   []WatchlistEntry
}
type WatchlistChange struct {
	ExpectedRevision int64
	Entries          []WatchlistEntry
}
type WatchlistPrice struct {
	Source          string
	SourceVersion   *string
	Price           string
	PriceAdjustment string
	ObservedAt      string
	FetchedAt       string
	RecordedAt      string
	Sample          bool
}
type WatchlistQuote struct {
	WatchlistEntry
	Status string
	Price  *WatchlistPrice
}
type WatchlistReport struct {
	PolicyVersion            string
	MaxObservationAgeSeconds int
	ObservedAt               string
	Watchlist                WatchlistState
	Quotes                   []WatchlistQuote
}
type SecurityPriceObservation struct {
	ObservationID       string
	Source              string
	SourceObservationID string
	InstrumentID        string
	Symbol              string
	Venue               string
	Currency            string
	Price               string
	PriceAdjustment     string
	ObservedAt          string
	FetchedAt           string
	RecordedAt          string
	OwnerVersion        string
	OwnerSample         bool
}
type Inputs struct {
	Cutoff time.Time
	State  WatchlistState
	Prices []SecurityPriceObservation
}

const RevisionLimit int64 = 9007199254740991

var ErrInput = errors.New("invalid watchlist input")
var ErrChanged = errors.New("watchlist revision changed")
