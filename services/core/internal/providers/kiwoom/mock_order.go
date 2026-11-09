package kiwoom

import (
	"context"
	"errors"
	"regexp"
)

var ErrMockOrderRejected = errors.New("limit order rejected")
var mockQuantity = regexp.MustCompile(`^[1-9][0-9]*$`)

type MockLimitOrder struct {
	AccountRef string
	Symbol     string
	Side       string
	Quantity   string
	LimitPrice string
}

type MockLimitOrderResult struct{ ProviderOrderRef string }

type kiwoomOrderResponse struct {
	kiwoomResult
	OrderNumber string `json:"ord_no"`
	Exchange    string `json:"dmst_stex_tp"`
}

func (c *Client) isMock() bool { return c != nil && c.environment == KiwoomMock }

func (c *Client) preflightMockLimitOrder(ctx context.Context) error {
	if !c.isMock() {
		return errors.New("Kiwoom mock limit order dispatch configuration is invalid")
	}
	if _, err := c.token(ctx); err != nil {
		return errors.New("limit order dispatch preflight failed")
	}
	return nil
}

func (c *Client) submitMockLimitOrder(ctx context.Context, request MockLimitOrder) (MockLimitOrderResult, error) {
	if !c.isMock() {
		return MockLimitOrderResult{}, errors.New("limit order dispatch configuration is invalid")
	}
	if !kiwoomStockPattern.MatchString(request.Symbol) || (request.Side != "BUY" && request.Side != "SELL") || len(request.Quantity) > 64 || !mockQuantity.MatchString(request.Quantity) || !positiveCanonicalDecimal(request.LimitPrice) || request.AccountRef == "" {
		return MockLimitOrderResult{}, errors.New("limit order dispatch request is invalid")
	}
	apiID := "kt10000"
	if request.Side == "SELL" {
		apiID = "kt10001"
	}
	page, err := c.postPage(ctx, apiID, kiwoomOrderPath, map[string]string{"dmst_stex_tp": string(KiwoomKRX), "stk_cd": request.Symbol, "ord_qty": request.Quantity, "ord_uv": request.LimitPrice, "trde_tp": "0", "cond_uv": ""}, "", "", false)
	if err != nil {
		return MockLimitOrderResult{}, classifyMockOrderError(err)
	}
	if page.continued || page.nextKey != "" {
		return MockLimitOrderResult{}, errors.New("limit order dispatch outcome is unknown")
	}
	var response kiwoomOrderResponse
	if kiwoomDecode(page.body, &response) != nil {
		return MockLimitOrderResult{}, errors.New("limit order dispatch outcome is unknown")
	}
	if err := kiwoomCheckResult(apiID, response.kiwoomResult); err != nil {
		return MockLimitOrderResult{}, classifyMockOrderError(err)
	}
	if !kiwoomOrderPattern.MatchString(response.OrderNumber) || (response.Exchange != "" && response.Exchange != string(KiwoomKRX)) {
		return MockLimitOrderResult{}, errors.New("limit order dispatch outcome is unknown")
	}
	return MockLimitOrderResult{ProviderOrderRef: c.orderAlias(request.AccountRef, response.OrderNumber)}, nil
}

func classifyMockOrderError(err error) error {
	var providerError *KiwoomError
	if errors.As(err, &providerError) && providerError.Kind == "provider_rejected" {
		return ErrMockOrderRejected
	}
	return errors.New("limit order dispatch outcome is unknown")
}

type MockOrders struct{ client *Client }

func NewMockOrders(client *Client) (*MockOrders, error) {
	if !client.isMock() {
		return nil, errors.New("Kiwoom mock limit order dispatch configuration is invalid")
	}
	return &MockOrders{client}, nil
}
func (d *MockOrders) Preflight(ctx context.Context) error {
	return d.client.preflightMockLimitOrder(ctx)
}
func (d *MockOrders) SubmitMockLimitOrder(ctx context.Context, request MockLimitOrder) (MockLimitOrderResult, error) {
	return d.client.submitMockLimitOrder(ctx, request)
}
