package ledger

import (
	"context"
	"errors"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
)

const maxLedgerExportRows = 10000

var ErrExportFailed = errors.New("ledger export failed; existing files were not overwritten; a completed export may remain after publication")

type ExportPublisher interface {
	Publish(context.Context, string, string, []c.LedgerActivity) (string, error)
}
type Export struct {
	Queries   c.Queries
	Publisher ExportPublisher
}

func (s Export) Run(ctx context.Context, dbPath, out string) (*c.ExportReceipt, error) {
	page, err := s.Queries.Activity(ctx, maxLedgerExportRows, nil)
	if err != nil || page.NextCursor != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrExportFailed
	}
	digest, err := s.Publisher.Publish(ctx, dbPath, out, page.Events)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), ErrExportFailed)
		}
		return nil, ErrExportFailed
	}
	return &c.ExportReceipt{SchemaVersion: "ledger-activity-export.v1", Source: page.Source, BrokerFreshness: page.BrokerFreshness, LedgerRevision: page.LedgerRevision, RecordedAt: page.RecordedAt, Rows: len(page.Events), SHA256: digest}, nil
}
