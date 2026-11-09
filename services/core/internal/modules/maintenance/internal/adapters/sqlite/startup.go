package sqlite

import (
	"context"
	"fmt"
	storage "omni-folio/services/core/internal/storage/contracts"
	"time"
)

func (s *Store) RequireCurrent(ctx context.Context) error {
	return requireSchemaVersionContext(ctx, s.db, latestSchema)
}
func (s *Store) RequireStartup(ctx context.Context) error {
	if err := s.RequireCurrent(ctx); err != nil {
		return err
	}
	owners, err := s.owners(s.db)
	if err != nil {
		return err
	}
	return owners.Observations.VerifyBackup(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		if err := owners.Ledger.VerifyStartup(ctx, ref); err != nil {
			return fmt.Errorf("ledger replay startup recovery: %w", err)
		}
		if err := owners.Trading.VerifyStartup(ctx, ref); err != nil {
			return err
		}
		if err := owners.Market.VerifyStartup(ctx, ref); err != nil {
			return err
		}
		if _, err := owners.Watchlist.VerifyBackup(ctx, ref); err != nil {
			return fmt.Errorf("watchlist startup recovery: %w", err)
		}
		return nil
	})
}
