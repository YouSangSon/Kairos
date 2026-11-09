package contracts

import (
	"context"
	storage "omni-folio/services/core/internal/storage/contracts"
)

type Imports interface {
	PreviewImport(context.Context, []byte) (*ImportPreview, *Error)
	ApplyImport(context.Context, ApplyRequest) (*ApplyReceipt, *Error)
}
type Queries interface {
	Status(context.Context) (Status, error)
	CanonicalInstrumentID(string) string
	Snapshot(context.Context) (*PortfolioSnapshot, error)
	Activity(context.Context, int, *ActivityCursor) (*LedgerActivityPage, error)
	InstrumentActivity(context.Context, string, string) (*InstrumentLedger, *Error)
	ValuationFacts(context.Context, storage.ObservationRef) (ValuationFacts, error)
	AdmittedPriceIdentities(context.Context, storage.ObservationRef) (AdmittedPrices, error)
}
type Broker interface {
	VerifyBrokerSync(context.Context) error
	CheckBrokerBinding(context.Context, string) error
	RecordMockBrokerSnapshot(context.Context, string, string, *BrokerSnapshot) (*BrokerKnownGoodSnapshot, error)
	RecordBrokerSnapshot(context.Context, string, *BrokerSnapshot) (*BrokerKnownGoodSnapshot, error)
	LatestBrokerSnapshot(context.Context, BrokerEnvironment, BrokerExchange, string) (*BrokerKnownGoodSnapshot, error)
	Reconciliation(context.Context) (*BrokerReconciliationView, error)
}
type Maintenance interface {
	VerifyStartup(context.Context, storage.ObservationRef) error
	VerifyBrokerBackup(context.Context, storage.ObservationRef) (BrokerRecovery, error)
	PreviewReplay(context.Context) (*ReplayPreview, error)
	ActivateReplay(context.Context, int64, string, string) (*ReplayPreview, error)
	VerifyBackup(context.Context, storage.ObservationRef, bool) (BackupProof, error)
}
type Service interface {
	Imports
	Queries
	Broker
	Maintenance
}
