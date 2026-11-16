package application

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
)

type Dispatcher interface {
	Provider() string
	Mode() string
	Preflight(context.Context) error
	SubmitLimitOrder(context.Context, OrderIntent) (string, error)
}

type DispatchJournal interface {
	LoadIntent(context.Context, string) (OrderIntent, error)
	AuthorizeOnce(context.Context, string, int64) (*OrderState, bool, error)
	RecordOutcome(context.Context, OrderEvent) (*OrderState, error)
}

func SubmitSyntheticLimit(ctx context.Context, journal DispatchJournal, id func(string) string, dispatch Dispatcher, orderID string, fencingToken int64) (*OrderState, error) {
	if journal == nil || id == nil || dispatch == nil || dispatch.Provider() == "" || dispatch.Mode() == "" || !safeOrderID(orderID) || fencingToken <= 0 {
		return nil, errors.New("limit order submit configuration is invalid")
	}
	intent, err := journal.LoadIntent(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if intent.Mode != "synthetic" {
		return nil, errors.New("limit order submit requires synthetic mode")
	}
	if dispatch.Provider() != intent.Provider || dispatch.Mode() != intent.Mode {
		return nil, errors.New("limit order submit capability does not match intent")
	}
	if err := dispatch.Preflight(ctx); err != nil {
		return nil, errors.New("limit order submit preflight failed")
	}
	state, dispatched, err := journal.AuthorizeOnce(ctx, orderID, fencingToken)
	if err != nil {
		return nil, err
	}
	if !dispatched {
		return state, errors.New("order submit was already dispatched; reconcile instead of resubmitting")
	}
	providerOrderRef, err := dispatch.SubmitLimitOrder(ctx, intent)
	if err != nil {
		if errors.Is(err, contracts.ErrLimitOrderRejected) {
			return journal.RecordOutcome(ctx, OrderEvent{
				EventID: id("order_event"), OrderID: orderID, Type: "SUBMIT_REJECTED", Source: "reconciliation",
			})
		}
		return state, errors.New("limit order submit outcome is unknown")
	}
	return journal.RecordOutcome(ctx, OrderEvent{
		EventID: id("order_event"), OrderID: orderID, Type: "SUBMIT_ACKNOWLEDGED", Source: "reconciliation",
		ProviderOrderRef: providerOrderRef,
	})
}
