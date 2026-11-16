package ledger

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
	"time"
)

type StoredBrokerSnapshot struct {
	ID, SHA256 string
	Snapshot   *ledgerdomain.BrokerSnapshot
}
type StoredBrokerReconciliation struct {
	Snapshot StoredBrokerSnapshot
	Record   *ledgerdomain.BrokerReconciliationRecord
}
type BrokerReader interface {
	LatestSnapshot(context.Context, contracts.BrokerEnvironment, contracts.BrokerExchange, string) (StoredBrokerReconciliation, error)
	LatestReconciliation(context.Context) (StoredBrokerReconciliation, error)
}
type BrokerJournal interface {
	PrepareMock(context.Context, string) error
	FindSnapshot(context.Context, *ledgerdomain.BrokerSnapshot) (StoredBrokerSnapshot, error)
	RecordSnapshot(context.Context, *ledgerdomain.BrokerSnapshot, string, string) (StoredBrokerSnapshot, error)
	LedgerPositions(context.Context, string) (int64, []ledgerdomain.BrokerLedgerEvent, error)
	FindReconciliation(context.Context, StoredBrokerSnapshot, string, int64) (*ledgerdomain.BrokerReconciliationRecord, error)
	RecordReconciliation(context.Context, StoredBrokerSnapshot, int64, *ledgerdomain.BrokerReconciliationRecord) (*ledgerdomain.BrokerReconciliationRecord, error)
}

func (s *UseCases) RecordBrokerSnapshot(ctx context.Context, account string, snapshot *contracts.BrokerSnapshot) (*contracts.BrokerKnownGoodSnapshot, error) {
	if err := ledgerdomain.RequireBrokerLedgerAccount(account); err != nil {
		return nil, err
	}
	input := brokerSnapshotDomain(snapshot)
	if err := ledgerdomain.ValidateBrokerSnapshot(input); err != nil {
		return nil, err
	}
	return s.recordBrokerSnapshot(ctx, account, "", input)
}
func (s *UseCases) RecordMockBrokerSnapshot(ctx context.Context, account, expected string, snapshot *contracts.BrokerSnapshot) (*contracts.BrokerKnownGoodSnapshot, error) {
	binding := ledgerdomain.MockBrokerBinding{LedgerAccount: account, ExpectedAccount: expected, SnapshotPresent: snapshot != nil}
	if snapshot != nil {
		binding.Environment, binding.SnapshotAccount = ledgerdomain.BrokerEnvironment(snapshot.Environment), snapshot.AccountRef
	}
	if err := ledgerdomain.ValidateMockBrokerBinding(binding); err != nil {
		return nil, err
	}
	input := brokerSnapshotDomain(snapshot)
	if err := ledgerdomain.ValidateBrokerSnapshot(input); err != nil {
		return nil, err
	}
	return s.recordBrokerSnapshot(ctx, account, expected, input)
}
func (s *UseCases) recordBrokerSnapshot(ctx context.Context, account, expected string, snapshot *ledgerdomain.BrokerSnapshot) (*contracts.BrokerKnownGoodSnapshot, error) {
	var result *contracts.BrokerKnownGoodSnapshot
	err := s.store.BrokerWrite(ctx, func(journal BrokerJournal) error {
		if expected != "" {
			if err := journal.PrepareMock(ctx, expected); err != nil {
				return err
			}
		}
		stored, err := journal.FindSnapshot(ctx, snapshot)
		if errors.Is(err, contracts.ErrNotFound) {
			id := s.id("broker_snapshot")
			stored, err = journal.RecordSnapshot(ctx, snapshot, id, s.now().UTC().Format(time.RFC3339Nano))
		}
		if err != nil {
			return err
		}
		revision, events, err := journal.LedgerPositions(ctx, account)
		if err != nil {
			return err
		}
		quantities, err := ledgerdomain.BrokerQuantities(events)
		if err != nil {
			return err
		}
		differences, allMatch, err := ledgerdomain.PositionDifferences(stored.Snapshot.Positions, quantities)
		if err != nil {
			return err
		}
		record, err := journal.FindReconciliation(ctx, stored, account, revision)
		if err == nil {
			if record.AllPositionsMatch != allMatch || !ledgerdomain.SamePositionDifferences(record.PositionDifferences, differences) {
				return errors.New("cached broker reconciliation differs from current ledger replay")
			}
		} else if errors.Is(err, contracts.ErrNotFound) {
			record = &ledgerdomain.BrokerReconciliationRecord{ReconciliationID: s.id("broker_reconciliation"), SnapshotID: stored.ID, LedgerAccountID: account,
				LedgerRevision: ledgerdomain.Revision(revision), RecordedAt: s.now().UTC().Format(time.RFC3339Nano), AllPositionsMatch: allMatch, PositionDifferences: differences}
			if err := ledgerdomain.ValidateBrokerReconciliation(record, stored.Snapshot); err != nil {
				return err
			}
			record, err = journal.RecordReconciliation(ctx, stored, revision, record)
			if err != nil {
				return err
			}
		} else {
			return err
		}
		result = knownGoodSnapshot(record, stored)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *UseCases) LatestBrokerSnapshot(ctx context.Context, environment contracts.BrokerEnvironment, exchange contracts.BrokerExchange, account string) (*contracts.BrokerKnownGoodSnapshot, error) {
	if err := ledgerdomain.ValidateBrokerQueryIdentity(ledgerdomain.BrokerEnvironment(environment), ledgerdomain.BrokerExchange(exchange), account); err != nil {
		return nil, err
	}
	var result *contracts.BrokerKnownGoodSnapshot
	err := s.store.BrokerRead(ctx, func(reader BrokerReader) error {
		stored, err := reader.LatestSnapshot(ctx, environment, exchange, account)
		if err != nil {
			return err
		}
		result = knownGoodSnapshot(stored.Record, stored.Snapshot)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *UseCases) Reconciliation(ctx context.Context) (*contracts.BrokerReconciliationView, error) {
	var result *contracts.BrokerReconciliationView
	err := s.store.BrokerRead(ctx, func(reader BrokerReader) error {
		stored, err := reader.LatestReconciliation(ctx)
		if err != nil {
			return err
		}
		snapshot, record := stored.Snapshot.Snapshot, stored.Record
		result = &contracts.BrokerReconciliationView{Provider: "kiwoom", Environment: contracts.BrokerEnvironment(snapshot.Environment), Exchange: contracts.BrokerExchange(snapshot.Exchange), Freshness: "unverified",
			FetchedAt: snapshot.FetchedAt, RecordedAt: record.RecordedAt, LedgerRevision: record.LedgerRevision, AllPositionsMatch: record.AllPositionsMatch, PositionDifferences: brokerDifferencesDTO(record.PositionDifferences)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func knownGoodSnapshot(record *ledgerdomain.BrokerReconciliationRecord, stored StoredBrokerSnapshot) *contracts.BrokerKnownGoodSnapshot {
	snapshot := stored.Snapshot
	return &contracts.BrokerKnownGoodSnapshot{SnapshotID: record.SnapshotID, ReconciliationID: record.ReconciliationID, Provider: "kiwoom", Environment: contracts.BrokerEnvironment(snapshot.Environment), Exchange: contracts.BrokerExchange(snapshot.Exchange), AccountRef: snapshot.AccountRef,
		LedgerAccountID: record.LedgerAccountID, FetchedAt: snapshot.FetchedAt, RecordedAt: record.RecordedAt, SnapshotSHA256: stored.SHA256, LedgerRevision: record.LedgerRevision, AllPositionsMatch: record.AllPositionsMatch,
		PositionDifferences: brokerDifferencesDTO(record.PositionDifferences), Snapshot: *brokerSnapshotDTO(snapshot)}
}
func brokerSnapshotDomain(input *contracts.BrokerSnapshot) *ledgerdomain.BrokerSnapshot {
	if input == nil {
		return nil
	}
	result := &ledgerdomain.BrokerSnapshot{Source: input.Source, Environment: ledgerdomain.BrokerEnvironment(input.Environment), Exchange: ledgerdomain.BrokerExchange(input.Exchange), AccountRef: input.AccountRef, MaskedAccount: input.MaskedAccount, FetchedAt: input.FetchedAt, Complete: input.Complete, Currency: input.Currency, Totals: ledgerdomain.BrokerTotals(input.Totals)}
	if input.Positions != nil {
		result.Positions = make([]ledgerdomain.BrokerPosition, len(input.Positions))
		for i, row := range input.Positions {
			result.Positions[i] = ledgerdomain.BrokerPosition(row)
		}
	}
	if input.OpenOrders != nil {
		result.OpenOrders = make([]ledgerdomain.BrokerOpenOrder, len(input.OpenOrders))
		for i, row := range input.OpenOrders {
			result.OpenOrders[i] = ledgerdomain.BrokerOpenOrder(row)
		}
	}
	return result
}
func brokerSnapshotDTO(input *ledgerdomain.BrokerSnapshot) *contracts.BrokerSnapshot {
	result := &contracts.BrokerSnapshot{Source: input.Source, Environment: contracts.BrokerEnvironment(input.Environment), Exchange: contracts.BrokerExchange(input.Exchange), AccountRef: input.AccountRef, MaskedAccount: input.MaskedAccount, FetchedAt: input.FetchedAt, Complete: input.Complete, Currency: input.Currency, Totals: contracts.BrokerTotals(input.Totals)}
	if input.Positions != nil {
		result.Positions = make([]contracts.BrokerPosition, len(input.Positions))
		for i, row := range input.Positions {
			result.Positions[i] = contracts.BrokerPosition(row)
		}
	}
	if input.OpenOrders != nil {
		result.OpenOrders = make([]contracts.BrokerOpenOrder, len(input.OpenOrders))
		for i, row := range input.OpenOrders {
			result.OpenOrders[i] = contracts.BrokerOpenOrder(row)
		}
	}
	return result
}
func brokerDifferencesDTO(input []ledgerdomain.BrokerPositionDifference) []contracts.BrokerPositionDifference {
	if input == nil {
		return nil
	}
	result := make([]contracts.BrokerPositionDifference, len(input))
	for i, row := range input {
		result[i] = contracts.BrokerPositionDifference(row)
	}
	return result
}
