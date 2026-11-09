package application

import (
	"encoding/json"
	"time"

	"omni-folio/services/core/internal/modules/research/contracts"
)

type Feed struct {
	SchemaVersion string
	GeneratedAt   string
	Since         string
	Until         string
	Sample        bool
	Omitted       contracts.PublicResearchOmitted
	Items         []json.RawMessage
}

type FeedReader interface{ Read() (*Feed, error) }

type Queries struct {
	Feeds FeedReader
	Now   func() time.Time
}

func (q *Queries) PublicResearch(market, symbol string) (contracts.PublicResearchView, error) {
	feed, err := q.Feeds.Read()
	if err != nil {
		return contracts.PublicResearchView{}, err
	}
	items := []json.RawMessage{}
	for _, rawItem := range feed.Items {
		var identity struct{ Market, Symbol string }
		_ = json.Unmarshal(rawItem, &identity)
		if identity.Market == market && identity.Symbol == symbol {
			items = append(items, rawItem)
		}
	}
	return contracts.PublicResearchView{
		SchemaVersion: "public-research-view.v1", FeedSchemaVersion: feed.SchemaVersion,
		ObservedAt: q.Now().UTC().Format(time.RFC3339Nano), Market: market, Symbol: symbol,
		GeneratedAt: feed.GeneratedAt, Since: feed.Since, Until: feed.Until, Sample: feed.Sample, Omitted: feed.Omitted, Items: items,
	}, nil
}
