package kiwoom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type mockOrderTransport func(*http.Request) (*http.Response, error)

func (f mockOrderTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMockSDKRejectsFractionalQuantityBeforeCredentials(t *testing.T) {
	client, err := NewClient(KiwoomMock, "app", "secret", make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client.transport = mockOrderTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network call")
	})
	orders, err := NewMockOrders(client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = orders.SubmitMockLimitOrder(context.Background(), MockLimitOrder{AccountRef: "account", Symbol: "005930", Side: "BUY", Quantity: "0.5", LimitPrice: "1.25"})
	if err == nil || calls != 0 {
		t.Fatalf("fractional quantity reached credentials: calls=%d err=%v", calls, err)
	}
}

func TestMockSDKKeepsDecimalPriceHMACAndNoWriteRetry(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, err := NewClient(KiwoomMock, "app", "secret", make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			client.accessToken = "test-token"
			client.tokenExpiresAt = client.now().Add(time.Hour)
			calls := 0
			client.transport = mockOrderTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Path != kiwoomOrderPath {
					t.Fatalf("unexpected API %s", request.URL.Path)
				}
				var payload map[string]string
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["ord_uv"] != "1.25" || payload["ord_qty"] != "2" || payload["dmst_stex_tp"] != "KRX" {
					t.Fatalf("protocol values changed %+v", payload)
				}
				body := `{"ord_no":"12345","dmst_stex_tp":"KRX","return_code":0}`
				if status == http.StatusUnauthorized {
					body = `{"return_code":8005}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Api-Id": {request.Header.Get("Api-Id")}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			orders, err := NewMockOrders(client)
			if err != nil {
				t.Fatal(err)
			}
			result, err := orders.SubmitMockLimitOrder(context.Background(), MockLimitOrder{AccountRef: "account-one", Symbol: "005930", Side: "BUY", Quantity: "2", LimitPrice: "1.25"})
			if calls != 1 {
				t.Fatalf("write retried %d times", calls)
			}
			if status == http.StatusOK {
				if err != nil || result.ProviderOrderRef != client.orderAlias("account-one", "12345") || result.ProviderOrderRef == client.orderAlias("account-two", "12345") {
					t.Fatalf("account alias changed %+v %v", result, err)
				}
			} else if err == nil || result.ProviderOrderRef != "" {
				t.Fatalf("unknown outcome accepted %+v %v", result, err)
			}
		})
	}
}

func TestMockSDKRejectsProductionBeforeCredentials(t *testing.T) {
	client, err := NewClient(KiwoomProduction, "app", "secret", make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewMockOrders(client); err == nil {
		t.Fatal("production client accepted by mock SDK")
	}
}
