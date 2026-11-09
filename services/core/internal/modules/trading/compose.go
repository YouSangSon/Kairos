package trading

import (
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/storage/observation"
	"time"

	"omni-folio/services/core/internal/modules/trading/contracts"
	broker "omni-folio/services/core/internal/modules/trading/internal/adapters/kiwoom"
	"omni-folio/services/core/internal/modules/trading/internal/adapters/sqlite"
	provider "omni-folio/services/core/internal/providers/kiwoom"
)

type Config struct {
	Observations     *observation.Registry
	MockBrokerClient *provider.Client
	DB               *sql.DB
	Clock            func() time.Time
	ID               func(string) string
	ExecutionOwner   string
	RunnerOwner      string
}

func Compose(config Config) (contracts.Trading, error) {
	if config.DB == nil || config.Clock == nil || config.ID == nil || config.ExecutionOwner == "" || config.RunnerOwner == "" {
		return nil, errors.New("trading configuration is incomplete")
	}
	store := sqlite.New(config.DB, config.Clock, config.ID, config.ExecutionOwner, config.RunnerOwner)
	if config.Observations != nil {
		if err := store.BindRecoveryReaders(config.Observations); err != nil {
			return nil, err
		}
	}
	if config.MockBrokerClient != nil {
		dispatch, err := broker.NewMockDispatch(config.MockBrokerClient)
		if err != nil {
			return nil, err
		}
		store.BindDispatcher(dispatch)
	}
	return store, nil
}
