package kiwoom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// ponytail: fixed KST is the credential-free K1 assumption; load IANA tzdata
// before pre-1988 history is promoted into a backtest dataset.
var kiwoomMarketLocation = time.FixedZone("Asia/Seoul", 9*60*60)

type kiwoomCandleRow struct {
	Close      string `json:"cur_prc"`
	Volume     string `json:"trde_qty"`
	Date       string `json:"dt"`
	ExecutedAt string `json:"cntr_tm"`
	Open       string `json:"open_pric"`
	High       string `json:"high_pric"`
	Low        string `json:"low_pric"`
}

type kiwoomCandleResponse struct {
	kiwoomResult
	StockCode  string            `json:"stk_cd"`
	TickBars   []kiwoomCandleRow `json:"stk_tic_chart_qry"`
	DailyBars  []kiwoomCandleRow `json:"stk_dt_pole_chart_qry"`
	MinuteBars []kiwoomCandleRow `json:"stk_min_pole_chart_qry"`
}

type KiwoomLatestTrade struct {
	Source      string            `json:"source"`
	Environment KiwoomEnvironment `json:"environment"`
	Exchange    KiwoomExchange    `json:"exchange"`
	Symbol      string            `json:"symbol"`
	Currency    string            `json:"currency"`
	Price       string            `json:"price"`
	ObservedAt  string            `json:"observed_at"`
	FetchedAt   string            `json:"fetched_at"`
}

func (c *Client) LatestTrade(ctx context.Context, symbol string) (*KiwoomLatestTrade, error) {
	if c == nil {
		return nil, &KiwoomError{Kind: "invalid_config"}
	}
	if !kiwoomStockPattern.MatchString(symbol) {
		return nil, &KiwoomError{Kind: "invalid_symbol"}
	}
	const apiID = "ka10079"
	page, err := c.readPage(ctx, apiID, map[string]string{
		"stk_cd": symbol, "tic_scope": "1", "upd_stkpc_tp": "0",
	}, "", "")
	if err != nil {
		return nil, err
	}
	var response kiwoomCandleResponse
	if err := kiwoomDecode(page.body, &response); err != nil || response.ReturnCode == nil {
		return nil, &KiwoomError{Kind: "invalid_response", APIID: apiID}
	}
	if err := kiwoomCheckResult(apiID, response.kiwoomResult); err != nil {
		return nil, err
	}
	responseSymbol, ok := kiwoomStockCode(response.StockCode)
	if !ok || responseSymbol != symbol {
		return nil, &KiwoomError{Kind: "symbol_mismatch", APIID: apiID}
	}
	if len(response.TickBars) == 0 {
		return nil, errMarketDataNotFound
	}
	latest, observedAt, ok := kiwoomNormalizeCandle(response.TickBars[0], true)
	if !ok {
		return nil, &KiwoomError{Kind: "invalid_trade", APIID: apiID}
	}
	previous := observedAt
	previousPrice := latest.Close
	for _, row := range response.TickBars[1:] {
		trade, at, ok := kiwoomNormalizeCandle(row, true)
		if !ok {
			return nil, &KiwoomError{Kind: "invalid_trade", APIID: apiID}
		}
		if at.After(previous) {
			return nil, &KiwoomError{Kind: "invalid_trade_order", APIID: apiID}
		}
		if at.Equal(previous) && trade.Close != previousPrice {
			return nil, &KiwoomError{Kind: "ambiguous_trade_time", APIID: apiID}
		}
		previous = at
		previousPrice = trade.Close
	}
	fetchedAt := c.now().UTC()
	if observedAt.After(fetchedAt) {
		return nil, &KiwoomError{Kind: "future_trade", APIID: apiID}
	}
	return &KiwoomLatestTrade{
		Source: "kiwoom", Environment: c.environment, Exchange: KiwoomKRX,
		Symbol: symbol, Currency: "KRW", Price: latest.Close,
		ObservedAt: observedAt.UTC().Format(time.RFC3339Nano), FetchedAt: fetchedAt.Format(time.RFC3339Nano),
	}, nil
}

func (c *Client) Candles(ctx context.Context, symbol, interval string) (*MarketDataSeries, error) {
	batch, err := c.readCandleBatch(ctx, symbol, interval)
	if err != nil {
		return nil, err
	}
	bars := make([]MarketDataBar, len(batch.bars))
	for i, bar := range batch.bars {
		bars[i] = MarketDataBar{At: bar.at, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume}
	}
	return &MarketDataSeries{
		Symbol: symbol, Venue: "XKRX", Timezone: "Asia/Seoul", Interval: interval,
		PriceAdjustment: marketDataAdjustmentProviderAdjusted, Bars: bars,
	}, nil
}

type kiwoomCapturedCandle struct {
	MockCapturedCandle
	at string
}

type kiwoomCandleBatch struct {
	request        MockCandleRequestEvidence
	fetchStartedAt time.Time
	fetchedAt      time.Time
	bars           []kiwoomCapturedCandle
}

func (c *Client) readCandleBatch(ctx context.Context, symbol, interval string) (*kiwoomCandleBatch, error) {
	return c.readCandlePages(ctx, symbol, interval, time.Time{}, nil)
}

func (c *Client) readCandlePages(ctx context.Context, symbol, interval string, startedAt time.Time, visit func(MockCandlePage) error) (*kiwoomCandleBatch, error) {
	if c == nil {
		return nil, &KiwoomError{Kind: "invalid_config"}
	}
	if !kiwoomStockPattern.MatchString(symbol) {
		return nil, &KiwoomError{Kind: "invalid_symbol"}
	}
	if startedAt.IsZero() {
		startedAt = c.now()
	}
	startedAt = startedAt.UTC()
	apiID, requestBody, minute := kiwoomCandleRequest(symbol, interval, startedAt.In(kiwoomMarketLocation).Format("20060102"))
	if apiID == "" {
		return nil, &KiwoomError{Kind: "unsupported_interval"}
	}

	request := MockCandleRequestEvidence{APIID: apiID, Body: requestBody}
	bars := make([]kiwoomCapturedCandle, 0, maxMarketDataRows)
	snapshot := func() *kiwoomCandleBatch {
		sort.Slice(bars, func(i, j int) bool { return bars[i].at < bars[j].at })
		latest := bars[max(0, len(bars)-maxMarketDataRows):]
		return &kiwoomCandleBatch{request: request, fetchStartedAt: startedAt, fetchedAt: c.now().UTC(), bars: latest}
	}

	seen := make(map[string]MarketDataBar)
	var previousProviderTime time.Time
	var pageRows []MockCapturedCandle
	var final *kiwoomCandleBatch
	var finishPage func(int, bool, bool) error
	if visit != nil {
		finishPage = func(number int, continued, complete bool) error {
			if complete && len(bars) == 0 {
				return errMarketDataNotFound
			}
			encoded, err := json.Marshal(struct {
				Domain    string                    `json:"domain"`
				Request   MockCandleRequestEvidence `json:"request"`
				Continued bool                      `json:"continued"`
				Rows      []MockCapturedCandle      `json:"rows"`
			}{"kiwoom-mock-candle-page.v1", request, continued, pageRows})
			if err != nil {
				return &KiwoomError{Kind: "invalid_candle_digest", APIID: apiID}
			}
			sum := sha256.Sum256(encoded)
			batch := snapshot()
			if batch.fetchedAt.Before(startedAt) {
				return &KiwoomError{Kind: "invalid_candle_clock", APIID: apiID}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if complete {
				final = batch
			}
			return visit(MockCandlePage{Number: number, Digest: hex.EncodeToString(sum[:]), Complete: complete, Batch: batch.mockBatch()})
		}
	}
	err := c.readPagesWithVisit(ctx, apiID, requestBody, func(body []byte) (bool, error) {
		// ponytail: read one page past the 500-bar cap to validate the provider's
		// page-boundary overlap; deeper history remains intentionally bounded.
		stopAfterPage := len(bars) >= maxMarketDataRows
		var response kiwoomCandleResponse
		if err := kiwoomDecode(body, &response); err != nil || response.ReturnCode == nil {
			return false, &KiwoomError{Kind: "invalid_response", APIID: apiID}
		}
		if err := kiwoomCheckResult(apiID, response.kiwoomResult); err != nil {
			return false, err
		}
		responseSymbol, ok := kiwoomStockCode(response.StockCode)
		if !ok || responseSymbol != symbol {
			return false, &KiwoomError{Kind: "symbol_mismatch", APIID: apiID}
		}
		rows, timeDigits := response.DailyBars, 8
		if minute {
			rows, timeDigits = response.MinuteBars, 14
		}
		if visit != nil {
			pageRows = make([]MockCapturedCandle, 0, len(rows))
		}
		for _, row := range rows {
			bar, at, ok := kiwoomNormalizeCandle(row, minute)
			if !ok {
				return false, &KiwoomError{Kind: "invalid_candle", APIID: apiID}
			}
			hasPrevious := !previousProviderTime.IsZero() || (visit != nil && len(seen) > 0)
			if hasPrevious && at.After(previousProviderTime) {
				return false, &KiwoomError{Kind: "invalid_candle_order", APIID: apiID}
			}
			previousProviderTime = at
			rawTime := row.Date
			if minute {
				rawTime = row.ExecutedAt
			}
			captured := MockCapturedCandle{ProviderTime: rawTime, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume}
			if visit != nil {
				if len(rawTime) != timeDigits || strings.Trim(rawTime, "0123456789") != "" {
					return false, &KiwoomError{Kind: "invalid_candle", APIID: apiID}
				}
				pageRows = append(pageRows, captured)
			}
			if existing, duplicate := seen[bar.At]; duplicate {
				if existing != bar {
					return false, &KiwoomError{Kind: "conflicting_candle", APIID: apiID}
				}
				continue
			}
			seen[bar.At] = bar
			bars = append(bars, kiwoomCapturedCandle{MockCapturedCandle: captured, at: bar.At})
		}
		return stopAfterPage, nil
	}, finishPage)
	if err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, errMarketDataNotFound
	}
	if final == nil {
		final = snapshot()
	}
	return final, nil
}

func kiwoomCandleRequest(symbol, interval, baseDate string) (string, MockCandleRequestBody, bool) {
	switch interval {
	case "1d":
		return "ka10081", MockCandleRequestBody{StockCode: symbol, BaseDate: baseDate, Adjustment: "1"}, false
	case "1m", "3m", "5m", "10m", "15m", "30m", "45m", "60m":
		return "ka10080", MockCandleRequestBody{StockCode: symbol, BaseDate: baseDate, Adjustment: "1", TickScope: interval[:len(interval)-1]}, true
	default:
		return "", MockCandleRequestBody{}, false
	}
}

func kiwoomNormalizeCandle(row kiwoomCandleRow, minute bool) (MarketDataBar, time.Time, bool) {
	rawTime, layout := row.Date, "20060102"
	if minute {
		rawTime, layout = row.ExecutedAt, "20060102150405"
	}
	at, err := time.ParseInLocation(layout, rawTime, kiwoomMarketLocation)
	if err != nil {
		return MarketDataBar{}, time.Time{}, false
	}
	open, openOK := kiwoomMagnitudeDecimal(row.Open)
	high, highOK := kiwoomMagnitudeDecimal(row.High)
	low, lowOK := kiwoomMagnitudeDecimal(row.Low)
	closeValue, closeOK := kiwoomMagnitudeDecimal(row.Close)
	volume, volumeOK := kiwoomNonNegativeDecimal(row.Volume)
	if !openOK || !highOK || !lowOK || !closeOK || !volumeOK {
		return MarketDataBar{}, time.Time{}, false
	}
	openNumber, openErr := parseDecimal(open)
	highNumber, highErr := parseDecimal(high)
	lowNumber, lowErr := parseDecimal(low)
	closeNumber, closeErr := parseDecimal(closeValue)
	if openErr != nil || highErr != nil || lowErr != nil || closeErr != nil ||
		openNumber.Sign() <= 0 || highNumber.Sign() <= 0 || lowNumber.Sign() <= 0 || closeNumber.Sign() <= 0 ||
		lowNumber.Cmp(openNumber) > 0 || lowNumber.Cmp(closeNumber) > 0 ||
		openNumber.Cmp(highNumber) > 0 || closeNumber.Cmp(highNumber) > 0 {
		return MarketDataBar{}, time.Time{}, false
	}
	return MarketDataBar{
		At: at.UTC().Format(time.RFC3339), Open: open, High: high, Low: low, Close: closeValue, Volume: volume,
	}, at, true
}
