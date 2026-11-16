package application

import (
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	watch "omni-folio/services/core/internal/modules/watchlist/contracts"
)

type BackupFacts struct {
	Ledger    ledger.BackupProof
	Broker    ledger.BrokerRecovery
	Market    market.RecoveryProof
	Trading   *trading.RecoverySnapshot
	Watchlist watch.RecoveryProof
}
