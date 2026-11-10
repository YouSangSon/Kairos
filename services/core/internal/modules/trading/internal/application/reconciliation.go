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
		if lookup.Provider != intent.Provider || lookup.Mode != intent.Mode || lookup.AccountRef != intent.AccountRef {
			return nil, false, errors.New("lookup authority does not match the order")
		}
		dispatchedAt, err := tx.Orders().DispatchTime(ctx, orderID)
		if err != nil {
			return nil, false, err
		}
		if observedAt.Before(dispatchedAt) {
			return nil, false, errors.New("lookup observation predates dispatch")
		}

		result := &KiwoomOrderReconciliation{State: state}
		if !lookup.Complete {
			result.Outcome = kiwoomLookupIncomplete
			return result, false, nil
		}
		if state.Status == "SUBMIT_UNKNOWN" && state.PendingAction == "SUBMIT" {
			result.Outcome = kiwoomLookupUncorrelated
			return result, false, nil
		}
		if len(state.ProviderOrderRefs) != 1 ||
			(state.Status != "OPEN" && state.Status != "PARTIALLY_FILLED" && state.Status != "FILLED" &&
				state.Status != "CANCEL_UNKNOWN" && state.Status != "CANCELED") {
			return nil, false, errors.New("order state cannot accept lookup reconciliation")
		}
		var match *domain.ValidatedOrderObservation
		for i := range observations {
			if observations[i].Order.ProviderOrderRef == state.ProviderOrderRefs[0] {
				match = &observations[i]
				break
			}
		}
		if match == nil {
			result.Outcome = kiwoomLookupNotFound
			return result, false, nil
		}
		order := match.Order
		if match.Submitted.Before(dispatchedAt) || match.Submitted.After(observedAt) || order.Symbol != intent.Symbol ||
			order.Exchange != intent.Exchange || order.Side != intent.Side || order.OrderType != intent.OrderType ||
			order.Currency != intent.Currency || order.Quantity != intent.Quantity || order.LimitPrice != intent.LimitPrice {
			return nil, false, errors.New("provider order observation conflicts with the local order")
		}
		if match.Incomplete {
			result.Outcome = kiwoomLookupIncomplete
			return result, false, nil
		}

		events, err := domain.ReconciliationEvents(orderID, (*orderdomain.State)(state), order)
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
