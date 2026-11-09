package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
	storage "omni-folio/services/core/internal/storage/contracts"
	"sort"
	"time"
)

func (s *Service) observed(ref storage.ObservationRef) (Querier, error) {
	handle, err := s.resolver.Resolve(ref)
	if err != nil {
		return nil, err
	}
	return observedQuery{handle}, nil
}
func (s *Service) ValuationFacts(ctx context.Context, ref storage.ObservationRef) (contracts.ValuationFacts, error) {
	q, err := s.observed(ref)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	if err := proveLedgerReplayState(ctx, q, false); err != nil {
		return contracts.ValuationFacts{}, err
	}
	rev, activity, err := s.proveLedgerEvents(ctx, q)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	snapshot, err := snapshotFrom(ctx, q)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	rawAsOf := zeroTime
	err = q.QueryRowContext(ctx, `SELECT occurred_at FROM events ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT 1`).Scan(&rawAsOf)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return contracts.ValuationFacts{}, err
	}
	asof, err := time.Parse(time.RFC3339Nano, rawAsOf)
	if err != nil || !codec.CanonicalUTC(rawAsOf) {
		return contracts.ValuationFacts{}, errors.New("ledger as_of is not canonical UTC")
	}
	recorded, err := time.Parse(time.RFC3339Nano, snapshot.RecordedAt)
	if err != nil || !codec.CanonicalUTC(snapshot.RecordedAt) {
		return contracts.ValuationFacts{}, errors.New("ledger recorded_at is not canonical UTC")
	}
	result := contracts.ValuationFacts{RestatedEvents: []contracts.ValuationEvent{}, ExternalFlows: []contracts.ValuationEvent{}, Snapshot: *snapshot, Revision: rev, ActivityRecordedAt: activity, AsOf: asof, RecordedAt: recorded, LastSplits: []contracts.SplitFact{}}
	rows, err := q.QueryContext(ctx, `SELECT instrument_id,currency,occurred_at FROM events WHERE type='SPLIT' ORDER BY rtrim(occurred_at,'Z'),sequence`)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	defer rows.Close()
	last := map[string]contracts.SplitFact{}
	for rows.Next() {
		var id, currency, raw string
		if err := rows.Scan(&id, &currency, &raw); err != nil {
			return contracts.ValuationFacts{}, err
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return contracts.ValuationFacts{}, err
		}
		last[id+"\x00"+currency] = contracts.SplitFact{InstrumentID: id, Currency: currency, OccurredAt: at}
	}
	if err := rows.Err(); err != nil {
		return contracts.ValuationFacts{}, err
	}
	if err := rows.Close(); err != nil {
		return contracts.ValuationFacts{}, err
	}
	keys := make([]string, 0, len(last))
	for key := range last {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.LastSplits = append(result.LastSplits, last[key])
	}

	cutoff, err := s.resolver.Cutoff(ref)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE sequence<=? AND rtrim(recorded_at,'Z')>rtrim(?,'Z'))`, rev, cutoff.UTC().Format(time.RFC3339Nano)).Scan(&result.FutureRecorded); err != nil {
		return contracts.ValuationFacts{}, err
	}
	restated, err := q.QueryContext(ctx, `SELECT e.type,e.occurred_at,COALESCE(e.instrument_id,''),COALESCE(e.symbol,''),COALESCE(e.quantity,''),e.currency,e.amount,COALESCE(e.counter_currency,''),COALESCE(e.counter_amount,'') FROM events e
 WHERE e.type<>'CASH_VOID' AND NOT EXISTS (SELECT 1 FROM events c WHERE c.type='CASH_VOID' AND c.account_id=e.account_id AND c.corrects_source_event_id=e.source_event_id)
 ORDER BY rtrim(e.occurred_at,'Z'),e.sequence`)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	for restated.Next() {
		var e contracts.ValuationEvent
		if err := restated.Scan(&e.Type, &e.OccurredAt, &e.InstrumentID, &e.Symbol, &e.Quantity, &e.Currency, &e.Amount, &e.CounterCurrency, &e.CounterAmount); err != nil {
			restated.Close()
			return contracts.ValuationFacts{}, err
		}
		result.RestatedEvents = append(result.RestatedEvents, e)
	}
	if err := restated.Err(); err != nil {
		restated.Close()
		return contracts.ValuationFacts{}, err
	}
	if err := restated.Close(); err != nil {
		return contracts.ValuationFacts{}, err
	}
	external, err := q.QueryContext(ctx, `SELECT e.occurred_at,e.currency,e.amount FROM events e
 WHERE e.type IN ('DEPOSIT','WITHDRAWAL') AND NOT EXISTS
 (SELECT 1 FROM events correction WHERE correction.account_id=e.account_id AND correction.corrects_source_event_id=e.source_event_id)
 ORDER BY rtrim(e.occurred_at,'Z'),e.sequence`)
	if err != nil {
		return contracts.ValuationFacts{}, err
	}
	for external.Next() {
		var e contracts.ValuationEvent
		if err := external.Scan(&e.OccurredAt, &e.Currency, &e.Amount); err != nil {
			external.Close()
			return contracts.ValuationFacts{}, err
		}
		result.ExternalFlows = append(result.ExternalFlows, e)
	}
	if err := external.Err(); err != nil {
		external.Close()
		return contracts.ValuationFacts{}, err
	}
	if err := external.Close(); err != nil {
		return contracts.ValuationFacts{}, err
	}
	return result, nil
}
func (s *Service) AdmittedPriceIdentities(ctx context.Context, ref storage.ObservationRef) (contracts.AdmittedPrices, error) {
	q, err := s.observed(ref)
	if err != nil {
		return contracts.AdmittedPrices{}, err
	}
	if err := proveLedgerReplayState(ctx, q, false); err != nil {
		return contracts.AdmittedPrices{}, err
	}
	revision, _, err := s.proveLedgerEvents(ctx, q)
	if err != nil {
		return contracts.AdmittedPrices{}, err
	}
	if _, err := snapshotFrom(ctx, q); err != nil {
		return contracts.AdmittedPrices{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT instrument_id,symbol,currency FROM events WHERE instrument_id IS NOT NULL AND instrument_id<>''`)
	if err != nil {
		return contracts.AdmittedPrices{}, err
	}
	defer rows.Close()
	admitted := map[string]contracts.PriceIdentity{}
	for rows.Next() {
		var id, symbol, currency string
		if err := rows.Scan(&id, &symbol, &currency); err != nil {
			return contracts.AdmittedPrices{}, errors.New("owner price ledger identity is invalid")
		}
		if ledgerdomain.InstrumentIDForSymbol(symbol) != id {
			return contracts.AdmittedPrices{}, errors.New("owner price ledger identity is not canonical")
		}
		admitted[id+"\x00"+currency] = contracts.PriceIdentity{InstrumentID: id, Currency: currency}
	}
	if err := rows.Err(); err != nil {
		return contracts.AdmittedPrices{}, err
	}
	result := contracts.AdmittedPrices{LedgerRevision: ledgerdomain.Revision(revision), Identities: []contracts.PriceIdentity{}}
	keys := make([]string, 0, len(admitted))
	for key := range admitted {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Identities = append(result.Identities, admitted[key])
	}
	return result, nil
}
func (s *Service) VerifyBackup(ctx context.Context, ref storage.ObservationRef, archival bool) (contracts.BackupProof, error) {
	q, err := s.observed(ref)
	if err != nil {
		return contracts.BackupProof{}, err
	}
	if err := proveLedgerReplayState(ctx, q, archival); err != nil {
		return contracts.BackupProof{}, err
	}
	if _, _, err := s.proveLedgerEvents(ctx, q); err != nil {
		return contracts.BackupProof{}, err
	}
	policy := fifoCostBasisPolicy
	if archival {
		policy = legacyFIFOCostBasisPolicy
	}
	snapshot, err := snapshotFromPolicy(ctx, q, policy, -1)
	if err != nil {
		return contracts.BackupProof{}, err
	}
	broker, err := proveBrokerRecovery(ctx, q)
	if err != nil {
		return contracts.BackupProof{}, err
	}
	return contracts.BackupProof{Snapshot: *snapshot, BrokerSHA256: broker.SHA256, BrokerSnapshots: broker.Snapshots, BrokerReconciliations: broker.Reconciliations}, nil
}

func (s *Service) VerifyBrokerBackup(ctx context.Context, ref storage.ObservationRef) (contracts.BrokerRecovery, error) {
	q, err := s.observed(ref)
	if err != nil {
		return contracts.BrokerRecovery{}, err
	}
	var revision int64
	if err := q.QueryRowContext(ctx, `SELECT revision FROM ledger_meta WHERE singleton=1`).Scan(&revision); err != nil {
		return contracts.BrokerRecovery{}, err
	}
	broker, err := proveBrokerRecovery(ctx, q)
	if err != nil {
		return contracts.BrokerRecovery{}, err
	}
	return contracts.BrokerRecovery{SHA256: broker.SHA256, Snapshots: broker.Snapshots, Reconciliations: broker.Reconciliations, LedgerRevision: revision}, nil
}
