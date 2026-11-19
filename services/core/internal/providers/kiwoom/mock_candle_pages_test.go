package kiwoom

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMockCandlePagesKeepsOriginalRequestAndCumulativeBatches(t *testing.T) {
	startedAt := mockSyncNow().Add(14 * time.Hour)
	clock := startedAt.Add(time.Hour)
	olderRow := strings.Replace(captureDailyRow, "20260824", "20260823", 1)
	payloads := []string{candleDailyBody(""), candleDailyBody(captureDailyRow), candleDailyBody(captureDailyRow + "," + olderRow), candleDailyBody("")}
	var calls int
	client := newMockCandlePageClient(t, func() time.Time { return clock }, func(request *http.Request) (*http.Response, error) {
		var body MockCandleRequestBody
		decodeKiwoomRequest(t, request, &body)
		if request.Header.Get("api-id") != "ka10081" || body != (MockCandleRequestBody{StockCode: "005930", BaseDate: "20300101", Adjustment: "1"}) {
			t.Fatalf("capture lost the original request after KST midnight: %#v", body)
		}
		header := map[string]string{"api-id": "ka10081"}
		if calls == 0 {
			assertNoKiwoomCursor(t, request)
		} else {
			assertKiwoomCursor(t, request, fmt.Sprint(calls))
		}
		if calls+1 < len(payloads) {
			header["cont-yn"], header["next-key"] = "Y", fmt.Sprint(calls+1)
		}
		payload := payloads[calls]
		calls++
		clock = clock.Add(time.Second)
		return kiwoomResponse(http.StatusOK, header, payload), nil
	})

	var pages []MockCandlePage
	batch, err := client.CapturePages(context.Background(), "005930", "1d", startedAt, func(page MockCandlePage) error {
		pages = append(pages, page)
		if page.Complete {
			clock = clock.Add(time.Second)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != len(payloads) || calls != len(payloads) {
		t.Fatalf("callbacks=%d chart calls=%d, want %d each", len(pages), calls, len(payloads))
	}
	for i, rows := range []int{0, 1, 2, 2} {
		page := pages[i]
		if page.Number != i+1 || page.Complete != (i+1 == len(pages)) || len(page.Batch.Bars) != rows ||
			!page.Batch.FetchStartedAt.Equal(startedAt) || !page.Batch.FetchedAt.Equal(startedAt.Add(time.Hour+time.Duration(i+1)*time.Second)) ||
			page.Batch.Request.Body.BaseDate != "20300101" {
			t.Fatalf("unexpected callback: %+v", page)
		}
	}
	if !reflect.DeepEqual(batch, pages[len(pages)-1].Batch) || batch.Bars[0].ProviderTime != "20260823" ||
		batch.Bars[1].ProviderTime != "20260824" || batch.Bars[1].Close != "1250" || batch.Bars[1].Volume != "1000" {
		t.Fatalf("unexpected final and callback records: %+v", batch)
	}
}

func TestMockCandlePageDigestCoversNormalizedRowsAndContinuation(t *testing.T) {
	stop := errors.New("visitor stopped capture")
	firstPage := func(t *testing.T, rows, cursor string, startedAt time.Time) MockCandlePage {
		t.Helper()
		calls := 0
		client := newMockCandlePageClient(t, mockSyncNow, func(*http.Request) (*http.Response, error) {
			calls++
			header := map[string]string{"api-id": "ka10081"}
			if cursor != "" {
				header["cont-yn"], header["next-key"] = "Y", cursor
			}
			return kiwoomResponse(http.StatusOK, header, candleDailyBody(rows)), nil
		})
		var page MockCandlePage
		batch, err := client.CapturePages(context.Background(), "005930", "1d", startedAt, func(value MockCandlePage) error {
			page = value
			return stop
		})
		if err != stop || calls != 1 || len(batch.Bars) != 0 || page.Complete != (cursor == "") {
			t.Fatalf("callback failure returned batch=%+v error=%v chart calls=%d", batch, err, calls)
		}
		digest, err := hex.DecodeString(page.Digest)
		if err != nil || len(digest) != 32 || page.Digest != strings.ToLower(page.Digest) {
			t.Fatalf("invalid SHA256 digest: %q", page.Digest)
		}
		return page
	}

	startedAt := mockSyncNow()
	base := firstPage(t, captureDailyRow, "volatile-cursor", startedAt)
	plain := `{"dt":"20260824","cur_prc":"1250","open_pric":"1200","high_pric":"1300","low_pric":"1150","trde_qty":"1000","ignored":"different provider metadata"}`
	if got := firstPage(t, plain, "different-cursor", startedAt.Add(-time.Minute)); got.Digest != base.Digest {
		t.Fatal("equivalent normalized page changed its digest")
	}
	for _, other := range []MockCandlePage{
		firstPage(t, captureDailyRow, "", startedAt),
		firstPage(t, captureDailyRow+","+plain, "same-cursor", startedAt),
		firstPage(t, strings.Replace(captureDailyRow, "20260824", "20260823", 1), "same-cursor", startedAt),
		firstPage(t, captureDailyRow, "same-cursor", startedAt.AddDate(0, 0, -1)),
	} {
		if other.Digest == base.Digest {
			t.Fatal("digest omitted continuation, duplicates, provider time, or semantic request")
		}
	}
}

func TestMockCandlePagesRejectsInvalidPageBeforeVisit(t *testing.T) {
	daily := candleDailyBody(captureDailyRow)
	conflict := candleDailyBody(strings.Replace(captureDailyRow, "250.00", "251.00", 1))
	newer := candleDailyBody(strings.Replace(captureDailyRow, "20260824", "20260825", 1))
	fraction := strings.Replace(captureDailyRow, `"dt":"20260824"`, `"cntr_tm":"20260824103000.123"`, 1)
	minute := func(rows string) string {
		return fmt.Sprintf(`{"stk_cd":"005930","stk_min_pole_chart_qry":[%s],"return_code":0}`, rows)
	}
	zeroTime := strings.Replace(fraction, "20260824103000.123", "00010101090000", 1)
	laterTime := strings.Replace(fraction, "20260824103000.123", "20260824103000", 1)
	for _, tc := range []struct {
		name, apiID, interval, invalidBody, kind string
		pages                                    int
		lastCursor                               string
		clockReverses                            bool
	}{
		{name: "repeated cursor", apiID: "ka10081", interval: "1d", invalidBody: daily, kind: "repeated_cursor", pages: 2, lastCursor: "1"},
		{name: "malformed continuation", apiID: "ka10081", interval: "1d", invalidBody: daily, kind: "invalid_pagination", pages: 1, lastCursor: "new\nkey"},
		{name: "page limit", apiID: "ka10081", interval: "1d", invalidBody: daily, kind: "pagination_limit", pages: 32, lastCursor: "32"},
		{name: "conflicting overlap", apiID: "ka10081", interval: "1d", invalidBody: conflict, kind: "conflicting_candle", pages: 2},
		{name: "cross-page order", apiID: "ka10081", interval: "1d", invalidBody: newer, kind: "invalid_candle_order", pages: 2},
		{name: "raw time width", apiID: "ka10080", interval: "1m", invalidBody: minute(fraction), kind: "invalid_candle", pages: 1},
		{name: "order after zero time", apiID: "ka10080", interval: "1m", invalidBody: minute(zeroTime + "," + laterTime), kind: "invalid_candle_order", pages: 1},
		{name: "reversed clock", apiID: "ka10081", interval: "1d", invalidBody: daily, kind: "invalid_candle_clock", pages: 1, clockReverses: true},
		{name: "empty terminal batch", apiID: "ka10081", interval: "1d", invalidBody: candleDailyBody(""), pages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := mockSyncNow()
			calls, visits := 0, 0
			client := newMockCandlePageClient(t, func() time.Time { return clock }, func(*http.Request) (*http.Response, error) {
				calls++
				header := map[string]string{"api-id": tc.apiID}
				body, cursor := daily, fmt.Sprint(calls)
				if calls == tc.pages {
					body, cursor = tc.invalidBody, tc.lastCursor
					if tc.clockReverses {
						clock = clock.Add(-time.Second)
					}
				}
				if cursor != "" {
					header["cont-yn"], header["next-key"] = "Y", cursor
				}
				return kiwoomResponse(http.StatusOK, header, body), nil
			})
			batch, err := client.CapturePages(context.Background(), "005930", tc.interval, mockSyncNow(), func(MockCandlePage) error {
				visits++
				return nil
			})
			if tc.kind == "" {
				if !errors.Is(err, errMarketDataNotFound) {
					t.Fatalf("empty terminal batch returned %v", err)
				}
			} else {
				assertKiwoomErrorKind(t, err, tc.kind)
			}
			if len(batch.Bars) != 0 || calls != tc.pages || visits != tc.pages-1 {
				t.Fatalf("invalid page returned bars=%d chart calls=%d callbacks=%d", len(batch.Bars), calls, visits)
			}
		})
	}
}

func TestMockCandlePagesStopsAfterOneOverlapPageButDigestsItsOlderRows(t *testing.T) {
	newest := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]kiwoomCandleRow, maxMarketDataRows+1)
	for i := range rows {
		rows[i] = kiwoomCandleRow{Date: newest.AddDate(0, 0, -i).Format("20060102"), Open: "10", High: "10", Low: "10", Close: "10", Volume: "1"}
	}
	payload := func(t *testing.T, rows []kiwoomCandleRow) string {
		t.Helper()
		body, err := json.Marshal(kiwoomCandleResponse{kiwoomResult: kiwoomResult{ReturnCode: new(int)}, StockCode: "005930", DailyBars: rows})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	firstPage := payload(t, rows[:maxMarketDataRows])
	var finalPages []MockCandlePage
	for _, test := range []struct {
		volume  string
		repeats bool
	}{{volume: "1"}, {volume: "2"}, {volume: "1", repeats: true}} {
		rows[maxMarketDataRows].Volume = test.volume
		olderPage := payload(t, rows[maxMarketDataRows-1:])
		calls := 0
		client := newMockCandlePageClient(t, mockSyncNow, func(*http.Request) (*http.Response, error) {
			calls++
			header := map[string]string{"api-id": "ka10081", "cont-yn": "Y", "next-key": fmt.Sprint(calls)}
			switch calls {
			case 1:
				return kiwoomResponse(http.StatusOK, header, firstPage), nil
			case 2:
				if test.repeats {
					header["next-key"] = "1"
				}
				return kiwoomResponse(http.StatusOK, header, olderPage), nil
			default:
				t.Fatal("read continued past the overlap-validation page")
				return nil, nil
			}
		})
		visits := 0
		batch, err := client.CapturePages(context.Background(), "005930", "1d", mockSyncNow(), func(page MockCandlePage) error {
			visits++
			if page.Complete {
				finalPages = append(finalPages, page)
			}
			return nil
		})
		if test.repeats {
			assertKiwoomErrorKind(t, err, "repeated_cursor")
			if calls != 2 || visits != 1 {
				t.Fatalf("invalid overlap page reached a callback: chart calls=%d callbacks=%d", calls, visits)
			}
			continue
		}
		if err != nil || calls != 2 || visits != 2 || len(batch.Bars) != maxMarketDataRows ||
			batch.Bars[0].ProviderTime != rows[maxMarketDataRows-1].Date || batch.Bars[maxMarketDataRows-1].ProviderTime != rows[0].Date {
			t.Fatalf("bounded result bars=%d chart calls=%d callbacks=%d error=%v", len(batch.Bars), calls, visits, err)
		}
	}
	if len(finalPages) != 2 || finalPages[0].Digest == finalPages[1].Digest || !reflect.DeepEqual(finalPages[0].Batch, finalPages[1].Batch) {
		t.Fatal("changed out-of-cap row did not change only the overlap page digest")
	}
}

func newMockCandlePageClient(t *testing.T, now func() time.Time, chart kiwoomRoundTripFunc) *MockCandles {
	t.Helper()
	transport := kiwoomRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == kiwoomTokenPath {
			return kiwoomResponse(http.StatusOK, nil, strings.Replace(syntheticTokenJSON("synthetic-token"), "20300102000000", "20310102000000", 1)), nil
		}
		return chart.RoundTrip(request)
	})
	client, err := NewMockCandles(MockCandlesConfig{AppKey: "synthetic-app-key", SecretKey: "synthetic-secret-key", AliasKey: []byte("0123456789abcdef0123456789abcdef"), Transport: transport, Now: now, Pacer: mockSyncPacer()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}
