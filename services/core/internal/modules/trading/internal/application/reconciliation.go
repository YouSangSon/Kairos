package application

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"time"
)

const kiwoomLookupMatched = "MATCHED"
const kiwoomLookupNotFound = "NOT_FOUND"
const kiwoomLookupIncomplete = "INCOMPLETE"
const kiwoomLookupUncorrelated = "UNCORRELATED"

func lookupToDomain(lookup KiwoomOrderLookup) domain.OrderLookup {
	result := domain.OrderLookup{Provider: lookup.Provider, Mode: lookup.Mode, AccountRef: lookup.AccountRef, ObservedAt: lookup.ObservedAt, Complete: lookup.Complete}
	if lookup.Orders != nil {
		result.Orders = make([]domain.OrderObservation, len(lookup.Orders))
	}
	for i, o := range lookup.Orders {
		next := domain.OrderObservation{ProviderOrderRef: o.ProviderOrderRef, Symbol: o.Symbol, Exchange: o.Exchange, Side: o.Side, OrderType: o.OrderType, Currency: o.Currency, Quantity: o.Quantity, LimitPrice: o.LimitPrice, RemainingQuantity: o.RemainingQuantity, SubmittedAt: o.SubmittedAt, ExecutionsComplete: o.ExecutionsComplete}
		if o.Executions != nil {
			next.Executions = make([]domain.ExecutionObservation, len(o.Executions))
		}
		for j, e := range o.Executions {
			next.Executions[j] = domain.ExecutionObservation(e)
		}
		result.Orders[i] = next
	}
	return result
}
func (s *UseCases) ReconcileKnownOrder(ctx context.Context, orderID string, lookup KiwoomOrderLookup) (*KiwoomOrderReconciliation, error) {
	if s == nil || s.journal == nil || !safeOrderID(orderID) {
		return nil, errors.New("invalid reconciliation target")
	}
	observedAt, observations, err := domain.ValidateKiwoomOrderLookup(lookupToDomain(lookup))
	if err != nil {
		return nil, err
	}
	return decideResult(ctx, s.journal, func(tx WriteScope) (*KiwoomOrderReconciliation, bool, error) {
		intent, err := tx.Orders().LoadOrderIntentFrom(ctx, orderID)
		if err != nil {
			return nil, false, err
		}
		state, err := tx.Orders().LoadOrderStateFrom(ctx, orderID)
		if err != nil {
			return nil, false, err
		}
		if err := domain.ValidateReconciliationAuthority(lookup.Provider, lookup.Mode, lookup.AccountRef, domain.OrderIntent(intent)); err != nil {
			return nil, false, err
		}
		dispatchedAt, err := tx.Orders().DispatchTime(ctx, orderID)
		if err != nil {
			return nil, false, err
		}
		if err := domain.ValidateReconciliationObservationTime(observedAt, dispatchedAt); err != nil {
			return nil, false, err
		}

		result := &KiwoomOrderReconciliation{State: state}
		outcome, err := domain.ReconciliationReadiness((*orderdomain.State)(state), lookup.Complete)
		if err != nil {
			return nil, false, err
		}
		if outcome != "" {
			result.Outcome = outcome
			return result, false, nil
		}
		match := domain.FindReconciliationObservation(observations, state.ProviderOrderRefs[0])
		if match == nil {
			result.Outcome = kiwoomLookupNotFound
			return result, false, nil
		}
		if err := domain.ValidateMatchedReconciliationObservation(*match, domain.OrderIntent(intent), dispatchedAt, observedAt); err != nil {
			return nil, false, err
		}
		if match.Incomplete {
			result.Outcome = kiwoomLookupIncomplete
			return result, false, nil
		}

		events, err := domain.ReconciliationEvents(orderID, (*orderdomain.State)(state), match.Order)
		if err != nil {
			return nil, false, err
		}
		for _, event := range events {
			if err := ValidateOrderEvent(OrderEvent(event)); err != nil {
				return nil, false, err
			}
		}
		recordedAt := s.now().UTC().Format(time.RFC3339Nano)
		for _, event := range events {
			state, err = tx.Orders().AppendOrderEventTx(ctx, OrderEvent(event), recordedAt)
			if err != nil {
				return nil, false, err
			}
		}
		return &KiwoomOrderReconciliation{Outcome: kiwoomLookupMatched, State: state}, true, nil
	})
}
