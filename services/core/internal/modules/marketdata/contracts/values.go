package contracts

type FXObservationInput struct {
	Source              string `json:"source"`
	SourceObservationID string `json:"source_observation_id"`
	BaseCurrency        string `json:"base_currency"`
	QuoteCurrency       string `json:"quote_currency"`
	Rate                string `json:"rate"`
	ObservedAt          string `json:"observed_at"`
	FetchedAt           string `json:"fetched_at"`
}

type FXObservation struct {
	ObservationID       string `json:"observation_id"`
	Source              string `json:"source"`
	SourceObservationID string `json:"source_observation_id"`
	BaseCurrency        string `json:"base_currency"`
	QuoteCurrency       string `json:"quote_currency"`
	Rate                string `json:"rate"`
	ObservedAt          string `json:"observed_at"`
	FetchedAt           string `json:"fetched_at"`
	RecordedAt          string `json:"recorded_at"`
	OwnerVersion        string `json:"-"`
	OwnerSample         bool   `json:"-"`
}

type SecurityPriceObservationInput struct {
	Source              string `json:"source"`
	SourceObservationID string `json:"source_observation_id"`
	InstrumentID        string `json:"instrument_id"`
	Symbol              string `json:"symbol"`
	Venue               string `json:"venue"`
	Currency            string `json:"currency"`
	Price               string `json:"price"`
	PriceAdjustment     string `json:"price_adjustment"`
	ObservedAt          string `json:"observed_at"`
	FetchedAt           string `json:"fetched_at"`
}

type SecurityPriceObservation struct {
	ObservationID       string `json:"observation_id"`
	Source              string `json:"source"`
	SourceObservationID string `json:"source_observation_id"`
	InstrumentID        string `json:"instrument_id"`
	Symbol              string `json:"symbol"`
	Venue               string `json:"venue"`
	Currency            string `json:"currency"`
	Price               string `json:"price"`
	PriceAdjustment     string `json:"price_adjustment"`
	ObservedAt          string `json:"observed_at"`
	FetchedAt           string `json:"fetched_at"`
	RecordedAt          string `json:"recorded_at"`
	OwnerVersion        string `json:"-"`
	OwnerSample         bool   `json:"-"`
}

type InstrumentListingInput struct {
	InstrumentID string
	Venue        string
	Symbol       string
	Currency     string
}

type InstrumentListingEvent struct {
	EventID                 string
	EventType               string
	Authority               string
	InstrumentID            string
	Venue                   string
	Symbol                  string
	Currency                string
	ExpectedPreviousEventID string
	RecordedAt              string
}

type OwnerFXPoint struct {
	ObservedAt string `json:"observed_at"`
	Rate       string `json:"rate"`
}

type OwnerFXSeriesInput struct {
	SchemaVersion string         `json:"schema_version"`
	SourceVersion string         `json:"source_version"`
	BaseCurrency  string         `json:"base_currency"`
	QuoteCurrency string         `json:"quote_currency"`
	Sample        bool           `json:"sample"`
	FetchedAt     string         `json:"fetched_at"`
	Points        []OwnerFXPoint `json:"points"`
}

type OwnerFXSeriesReceipt struct {
	StorageCheck    string  `json:"storage_check"`
	Authority       string  `json:"authority"`
	SourceVersion   string  `json:"source_version"`
	BaseCurrency    string  `json:"base_currency"`
	QuoteCurrency   string  `json:"quote_currency"`
	Sample          bool    `json:"sample"`
	DataSHA256      string  `json:"data_sha256"`
	PointCount      int     `json:"point_count"`
	FirstObservedAt *string `json:"first_observed_at"`
	LastObservedAt  *string `json:"last_observed_at"`
	FetchedAt       string  `json:"fetched_at"`
	RecordedAt      string  `json:"recorded_at"`
}

type OwnerPricePoint struct {
	ObservedAt string `json:"observed_at"`
	Price      string `json:"price"`
}

type OwnerPriceSeriesInput struct {
	SchemaVersion   string            `json:"schema_version"`
	SourceVersion   string            `json:"source_version"`
	InstrumentID    string            `json:"instrument_id"`
	Symbol          string            `json:"symbol"`
	Venue           string            `json:"venue"`
	PriceAdjustment string            `json:"price_adjustment"`
	Currency        string            `json:"currency"`
	Sample          bool              `json:"sample"`
	FetchedAt       string            `json:"fetched_at"`
	Points          []OwnerPricePoint `json:"points"`
}

type OwnerPriceSeriesReceipt struct {
	StorageCheck    string  `json:"storage_check"`
	Authority       string  `json:"authority"`
	SourceVersion   string  `json:"source_version"`
	InstrumentID    string  `json:"instrument_id"`
	Symbol          string  `json:"symbol"`
	Venue           string  `json:"venue"`
	PriceAdjustment string  `json:"price_adjustment"`
	Currency        string  `json:"currency"`
	Sample          bool    `json:"sample"`
	DataSHA256      string  `json:"data_sha256"`
	PointCount      int     `json:"point_count"`
	FirstObservedAt *string `json:"first_observed_at"`
	LastObservedAt  *string `json:"last_observed_at"`
	FetchedAt       string  `json:"fetched_at"`
	RecordedAt      string  `json:"recorded_at"`
}

type BenchmarkIndexPoint struct {
	ObservedAt string `json:"observed_at"`
	Level      string `json:"level"`
}

type BenchmarkSeriesInput struct {
	SchemaVersion   string                `json:"schema_version"`
	BenchmarkID     string                `json:"benchmark_id"`
	Label           string                `json:"label"`
	Currency        string                `json:"currency"`
	ReturnBasis     string                `json:"return_basis"`
	SourceReference string                `json:"source_reference"`
	SourceVersion   string                `json:"source_version"`
	Sample          bool                  `json:"sample"`
	FetchedAt       string                `json:"fetched_at"`
	Points          []BenchmarkIndexPoint `json:"points"`
}

type BenchmarkSeriesReceipt struct {
	StorageCheck    string `json:"storage_check"`
	Authority       string `json:"authority"`
	BenchmarkID     string `json:"benchmark_id"`
	Label           string `json:"label"`
	Currency        string `json:"currency"`
	ReturnBasis     string `json:"return_basis"`
	SourceReference string `json:"source_reference"`
	SourceVersion   string `json:"source_version"`
	Sample          bool   `json:"sample"`
	DataSHA256      string `json:"data_sha256"`
	PointCount      int    `json:"point_count"`
	FirstObservedAt string `json:"first_observed_at"`
	LastObservedAt  string `json:"last_observed_at"`
	FetchedAt       string `json:"fetched_at"`
	RecordedAt      string `json:"recorded_at"`
}

type MarketDataSeries struct {
	InstrumentID    *string
	Currency        *string
	Symbol          string
	Venue           string
	Timezone        string
	Interval        string
	PriceAdjustment string
	Bars            []MarketDataBar
}

type MarketDataBar struct {
	At     string `json:"at"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}
