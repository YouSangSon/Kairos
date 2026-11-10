package ledger

import (
	"context"
	"errors"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
)

type MockSnapshot interface {
	AccountRef() string
	Snapshot(context.Context) (*c.BrokerSnapshot, error)
	Close()
}
type MockSnapshotFactory interface {
	Open(context.Context) (MockSnapshot, error)
}
type MockSync struct {
	Broker  c.Broker
	Factory MockSnapshotFactory
}

func (s MockSync) Run(ctx context.Context, account string) (*c.MockSyncReceipt, error) {
	if err := s.Broker.VerifyBrokerSync(ctx); err != nil {
		return nil, MockSyncFailure(ctx, "database recovery failed")
	}
	if err := s.Broker.CheckBrokerBinding(ctx, ""); err != nil {
		return nil, MockSyncFailure(ctx, "stored broker account binding differs")
	}
	reader, err := s.Factory.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	expected := reader.AccountRef()
	if err := s.Broker.CheckBrokerBinding(ctx, expected); err != nil {
		return nil, MockSyncFailure(ctx, "stored broker account binding differs")
	}
	snapshot, err := reader.Snapshot(ctx)
	if err != nil {
		return nil, MockSyncFailure(ctx, "complete provider read failed; previous snapshot retained")
	}
	if snapshot.AccountRef != expected {
		return nil, MockSyncFailure(ctx, "provider account differs; previous snapshot retained")
	}
	stored, err := s.Broker.RecordMockBrokerSnapshot(ctx, account, expected, snapshot)
	if err != nil {
		return nil, MockSyncFailure(ctx, "could not verify and store snapshot")
	}
	return &c.MockSyncReceipt{Provider: stored.Provider, Mode: stored.Environment, FetchedAt: stored.FetchedAt, RecordedAt: stored.RecordedAt, LedgerRevision: stored.LedgerRevision, Positions: len(stored.Snapshot.Positions), OpenOrders: len(stored.Snapshot.OpenOrders), AllPositionsMatch: stored.AllPositionsMatch}, nil
}
func MockSyncFailure(ctx context.Context, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("kiwoom mock sync " + message)
}
