package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/application"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	watch "omni-folio/services/core/internal/modules/watchlist/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
)

type Owners struct {
	Observations *observation.Registry
	Ledger       ledger.Maintenance
	Market       market.Recovery
	Trading      trading.Recovery
	Watchlist    watch.Recovery
}
type OwnerFactory func(*sql.DB) (Owners, error)

func (s *Store) owners(db *sql.DB) (Owners, error) {
	owners, err := s.factory(db)
	if err != nil {
		return Owners{}, err
	}
	if !owners.Observations.MatchesDB(db) || owners.Ledger == nil || owners.Market == nil || owners.Trading == nil || owners.Watchlist == nil {
		return Owners{}, errors.New("backup owners do not belong to the candidate database")
	}
	return owners, nil
}
func (s *Store) readProofs(ctx context.Context, db *sql.DB, schema int, archival, fullLedger bool) (application.BackupFacts, error) {
	owners, err := s.owners(db)
	if err != nil {
		return application.BackupFacts{}, err
	}
	result := application.BackupFacts{}
	err = owners.Observations.VerifyBackup(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		var err error
		if fullLedger {
			result.Ledger, err = owners.Ledger.VerifyBackup(ctx, ref, archival)
			if err != nil {
				return fmt.Errorf("ledger backup proof: %w", err)
			}
			result.Broker = ledger.BrokerRecovery{SHA256: result.Ledger.BrokerSHA256, Snapshots: result.Ledger.BrokerSnapshots, Reconciliations: result.Ledger.BrokerReconciliations}
		} else {
			result.Broker, err = owners.Ledger.VerifyBrokerBackup(ctx, ref)
			if err != nil {
				return fmt.Errorf("broker backup proof: %w", err)
			}
		}
		result.Trading, err = owners.Trading.VerifyBackup(ctx, ref, trading.BackupQuery{Schema: schema, Archival: archival})
		if err != nil {
			return fmt.Errorf("trading backup proof: %w", err)
		}
		result.Market, err = owners.Market.VerifyBackup(ctx, ref, market.BackupQuery{Schema: schema})
		if err != nil {
			return fmt.Errorf("market backup proof: %w", err)
		}
		if schema >= 26 {
			result.Watchlist, err = owners.Watchlist.VerifyBackup(ctx, ref)
			if err != nil {
				return fmt.Errorf("watchlist backup proof: %w", err)
			}
		}
		return nil
	})
	return result, err
}
func (s *Store) migrateCandidate(ctx context.Context, db *sql.DB) error {
	owners, err := s.owners(db)
	if err != nil {
		return err
	}
	return New(db, owners.Observations, owners.Trading, s.now, s.id, s.factory).Migrate(ctx)
}
