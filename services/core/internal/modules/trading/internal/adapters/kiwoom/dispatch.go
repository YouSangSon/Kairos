package kiwoom

import (
	"context"
	"errors"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	provider "omni-folio/services/core/internal/providers/kiwoom"
)

type MockDispatch struct{ orders *provider.MockOrders }

func NewMockDispatch(client *provider.Client) (*MockDispatch, error) {
	orders, err := provider.NewMockOrders(client)
	if err != nil {
		return nil, err
	}
	return &MockDispatch{orders: orders}, nil
}

func (*MockDispatch) Provider() string { return "kiwoom" }
func (*MockDispatch) Mode() string     { return "synthetic" }

func (d *MockDispatch) Preflight(ctx context.Context) error {
	if d == nil || d.orders == nil {
		return errors.New("Kiwoom mock limit order dispatch configuration is invalid")
	}
	if err := d.orders.Preflight(ctx); err != nil {
		return errors.New("limit order dispatch preflight failed")
	}
	return nil
}

func (d *MockDispatch) SubmitLimitOrder(ctx context.Context, intent contracts.OrderIntent) (string, error) {
	if d == nil || d.orders == nil {
		return "", errors.New("limit order dispatch configuration is invalid")
	}
	if domain.ValidateOrderIntent(domain.OrderIntent(intent)) != nil || intent.Mode != "synthetic" {
		return "", errors.New("limit order dispatch request is invalid")
	}
	result, err := d.orders.SubmitMockLimitOrder(ctx, provider.MockLimitOrder{
		AccountRef: intent.AccountRef, Symbol: intent.Symbol, Side: intent.Side, Quantity: intent.Quantity, LimitPrice: intent.LimitPrice,
	})
	if err != nil {
		if errors.Is(err, provider.ErrMockOrderRejected) {
			return "", contracts.ErrLimitOrderRejected
		}
		return "", errors.New("limit order dispatch outcome is unknown")
	}
	return result.ProviderOrderRef, nil
}
