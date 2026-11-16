package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"time"
)

const (
	kiwoomLookupMatched      = "MATCHED"
	kiwoomLookupNotFound     = "NOT_FOUND"
	kiwoomLookupIncomplete   = "INCOMPLETE"
	kiwoomLookupUncorrelated = "UNCORRELATED"
)

type KiwoomOrderLookup = contracts.OrderLookup

type KiwoomOrderObservation = contracts.OrderObservation

type KiwoomExecutionObservation = contracts.ExecutionObservation

type KiwoomOrderReconciliation = contracts.OrderReconciliation

func (s *Store) ReconcileKnownOrder(ctx context.Context, id string, lookup KiwoomOrderLookup) (*KiwoomOrderReconciliation, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invalid reconciliation target")
	}
	return s.business().ReconcileKnownOrder(ctx, id, lookup)
}

func orderDispatchTime(ctx context.Context, q orderQuerier, orderID string) (time.Time, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT recorded_at FROM order_events WHERE order_id=? AND event_type='SUBMIT_DISPATCHED' ORDER BY sequence LIMIT 1`, orderID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, errors.New("order has no submit dispatch")
	}
	if err != nil {
		return time.Time{}, err
	}
	value, ok := canonicalUTCTime(raw)
	if !ok {
		return time.Time{}, errors.New("stored submit dispatch time is invalid")
	}
	return value, nil
}

var positiveCanonicalDecimal = domain.PositiveCanonicalDecimal

var validOrderNonNegativeInteger = domain.ValidOrderNonNegativeInteger

var canonicalUTCTime = domain.CanonicalUTCTime

var reconciliationEventID = domain.ReconciliationEventID
