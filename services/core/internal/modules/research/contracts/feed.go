package contracts

import (
	"encoding/json"
	"errors"
)

type PublicResearchOmitted struct {
	UnscopedNews          uint64 `json:"unscoped_news"`
	MissingSourceEvidence uint64 `json:"missing_source_evidence"`
}

type PublicResearchView struct {
	SchemaVersion     string                `json:"schema_version"`
	FeedSchemaVersion string                `json:"feed_schema_version"`
	ObservedAt        string                `json:"observed_at"`
	Market            string                `json:"market"`
	Symbol            string                `json:"symbol"`
	GeneratedAt       string                `json:"generated_at"`
	Since             string                `json:"since"`
	Until             string                `json:"until"`
	Sample            bool                  `json:"sample"`
	Omitted           PublicResearchOmitted `json:"omitted"`
	Items             []json.RawMessage     `json:"items"`
}

var (
	ErrNotConfigured   = errors.New("public research is not configured")
	ErrFeedMissing     = errors.New("public research file is missing")
	ErrFeedUnavailable = errors.New("public research file is unavailable")
)

type Queries interface {
	PublicResearch(market, symbol string) (PublicResearchView, error)
}
