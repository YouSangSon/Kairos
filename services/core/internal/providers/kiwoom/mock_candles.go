package kiwoom

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

type MockCandlesConfig struct {
	AppKey, SecretKey string
	AliasKey          []byte
	Transport         http.RoundTripper
	Now               func() time.Time
	Pacer             *MockReadPacer
}

type MockCandleRequestBody struct {
	StockCode  string `json:"stk_cd"`
	BaseDate   string `json:"base_dt"`
	Adjustment string `json:"upd_stkpc_tp"`
	TickScope  string `json:"tic_scope,omitempty"`
}
type MockCandleRequestEvidence struct {
	APIID string                `json:"api_id"`
	Body  MockCandleRequestBody `json:"body"`
}
type MockCapturedCandle struct {
	ProviderTime string `json:"provider_time"`
	Open         string `json:"open"`
	High         string `json:"high"`
	Low          string `json:"low"`
	Close        string `json:"close"`
	Volume       string `json:"volume"`
}
type MockCandleBatch struct {
	Request        MockCandleRequestEvidence
	FetchStartedAt time.Time
	FetchedAt      time.Time
	Bars           []MockCapturedCandle
}

// MockCandlePage reports a validated provider page and the cumulative capture.
type MockCandlePage struct {
	Number   int
	Digest   string
	Complete bool
	Batch    MockCandleBatch
}

type MockCandles struct {
	mu     sync.Mutex
	client *Client
}

func NewMockCandles(config MockCandlesConfig) (*MockCandles, error) {
	if config.Transport == nil || config.Now == nil || !config.Pacer.valid() {
		return nil, errors.New("Kiwoom mock candle configuration is invalid")
	}
	client, err := NewClient(KiwoomMock, config.AppKey, config.SecretKey, config.AliasKey)
	if err != nil {
		return nil, err
	}
	client.now = config.Now
	client.transport = &mockCandleTransport{base: config.Transport, pacer: config.Pacer}
	return &MockCandles{client: client}, nil
}

func ValidateMockCandleRequest(symbol, interval string) error {
	if !kiwoomStockPattern.MatchString(symbol) {
		return errors.New("kiwoom mock candle symbol is invalid")
	}
	if apiID, _, _ := kiwoomCandleRequest(symbol, interval, ""); apiID == "" {
		return errors.New("kiwoom mock candle interval is unsupported")
	}
	return nil
}

func (c *MockCandles) Capture(ctx context.Context, symbol, interval string) (MockCandleBatch, error) {
	return c.capture(ctx, symbol, interval, time.Time{}, nil)
}

// CapturePages starts from page one using the original request clock and visits
// each validated page before fetching another. A visitor error ends the capture.
func (c *MockCandles) CapturePages(ctx context.Context, symbol, interval string, startedAt time.Time, visit func(MockCandlePage) error) (MockCandleBatch, error) {
	if startedAt.IsZero() || visit == nil {
		return MockCandleBatch{}, errors.New("Kiwoom mock candle page capture configuration is invalid")
	}
	return c.capture(ctx, symbol, interval, startedAt, visit)
}

func (c *MockCandles) capture(ctx context.Context, symbol, interval string, startedAt time.Time, visit func(MockCandlePage) error) (MockCandleBatch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return MockCandleBatch{}, errors.New("Kiwoom mock candle capture is closed")
	}
	var terminal MockCandleBatch
	if visit != nil {
		notify := visit
		visit = func(page MockCandlePage) error {
			if page.Complete {
				terminal = page.Batch
			}
			return notify(page)
		}
	}
	batch, err := c.client.readCandlePages(ctx, symbol, interval, startedAt, visit)
	if err != nil {
		return MockCandleBatch{}, err
	}
	if visit != nil {
		return terminal, nil
	}
	return batch.mockBatch(), nil
}

func (batch *kiwoomCandleBatch) mockBatch() MockCandleBatch {
	result := MockCandleBatch{Request: batch.request, FetchStartedAt: batch.fetchStartedAt, FetchedAt: batch.fetchedAt, Bars: make([]MockCapturedCandle, len(batch.bars))}
	for i, bar := range batch.bars {
		result.Bars[i] = bar.MockCapturedCandle
	}
	return result
}

func (c *MockCandles) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return
	}
	clear(c.client.aliasKey)
	c.client.appKey, c.client.secretKey, c.client.accessToken = "", "", ""
	c.client.aliasKey, c.client.transport, c.client.now = nil, nil, nil
	c.client = nil
}

type mockCandleTransport struct {
	base  http.RoundTripper
	pacer *MockReadPacer
}

func (t *mockCandleTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.Scheme != "https" || request.URL.Host != "mockapi.kiwoom.com" || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.User != nil || !((request.URL.Path == kiwoomTokenPath && request.Header.Get("api-id") == "") || (request.URL.Path == kiwoomChartPath && (request.Header.Get("api-id") == "ka10080" || request.Header.Get("api-id") == "ka10081"))) {
		return nil, &KiwoomError{Kind: "invalid_request"}
	}
	return t.pacer.roundTrip(request, t.base)
}
