package kiwoom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"omni-folio/services/core/internal/modules/trading/contracts"
	provider "omni-folio/services/core/internal/providers/kiwoom"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockDispatch(t *testing.T, orderResponse string, status int) (*MockDispatch, *int, *map[string]string) {
	t.Helper()
	calls := new(int)
	body := new(map[string]string)
	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "mockapi.kiwoom.com" {
			t.Fatalf("unexpected provider endpoint %s", r.URL.Host)
		}
		if r.URL.Path == "/oauth2/token" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"expires_dt":"20990102000000","token_type":"Bearer","token":"synthetic-token","return_code":0}`))}, nil
		}
		if r.URL.Path != "/api/dostk/ordr" {
			t.Fatalf("unexpected provider path %s", r.URL.Path)
		}
		*calls++
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Api-Id": []string{"kt10000"}}, Body: io.NopCloser(strings.NewReader(orderResponse))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	client, err := provider.NewClient(provider.KiwoomMock, "synthetic-app-key", "synthetic-secret-key", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := NewMockDispatch(client)
	if err != nil {
		t.Fatal(err)
	}
	return dispatch, calls, body
}

func syntheticIntent() contracts.OrderIntent {
	return contracts.OrderIntent{ClientOrderID: "client-adapter", Provider: "kiwoom", Mode: "synthetic", AccountRef: "kiwoom_account_AAAAAAAAAAAAAAAAAAAAAAAA", Symbol: "005930", Exchange: "KRX", Side: "BUY", OrderType: "LIMIT", Quantity: "10", LimitPrice: "70000.25", Currency: "KRW"}
}

func TestMockDispatchValidatesFullIntentBeforeProviderCall(t *testing.T) {
	dispatch, calls, _ := mockDispatch(t, `{"return_code":0,"ord_no":"0000024","dmst_stex_tp":"KRX"}`, 200)
	intent := syntheticIntent()
	intent.StrategyResultSHA256 = "invalid"
	if _, err := dispatch.SubmitLimitOrder(context.Background(), intent); err == nil || *calls != 0 {
		t.Fatalf("invalid strategy reached provider: calls=%d err=%v", *calls, err)
	}
	intent = syntheticIntent()
	intent.Mode = "paper"
	if _, err := dispatch.SubmitLimitOrder(context.Background(), intent); err == nil || *calls != 0 {
		t.Fatalf("paper mode reached provider: calls=%d err=%v", *calls, err)
	}
}

func TestMockDispatchPreservesDecimalLimitAndAccountScopedOrderAlias(t *testing.T) {
	dispatch, calls, body := mockDispatch(t, `{"return_code":0,"ord_no":"0000024","dmst_stex_tp":"KRX"}`, 200)
	if err := dispatch.Preflight(context.Background()); err != nil {
		t.Fatal(err)
	}
	ref, err := dispatch.SubmitLimitOrder(context.Background(), syntheticIntent())
	if err != nil || ref != "kiwoom_order_DwrX0mSdocYw8ENh9XQBhszn" {
		t.Fatalf("provider order alias=%q err=%v", ref, err)
	}
	if *calls != 1 || (*body)["ord_uv"] != "70000.25" || (*body)["ord_qty"] != "10" || (*body)["dmst_stex_tp"] != "KRX" {
		t.Fatalf("calls=%d request=%v", *calls, *body)
	}
}

func TestMockDispatchRejectsOnlyDefinitiveProviderRejection(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		rejected   bool
	}{
		{"provider rejection", `{"return_code":42,"return_msg":"raw-provider-message"}`, 200, true},
		{"unauthorized", `{"return_code":42}`, 401, false},
		{"malformed", `{`, 200, false},
		{"wrong exchange", `{"return_code":0,"ord_no":"0000024","dmst_stex_tp":"NXT"}`, 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dispatch, calls, _ := mockDispatch(t, test.body, test.status)
			if err := dispatch.Preflight(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, err := dispatch.SubmitLimitOrder(context.Background(), syntheticIntent())
			if err == nil || errors.Is(err, contracts.ErrLimitOrderRejected) != test.rejected || strings.Contains(err.Error(), "raw-provider-message") {
				t.Fatalf("rejection=%v err=%v", test.rejected, err)
			}
			if *calls != 1 {
				t.Fatalf("submit retried %d times", *calls)
			}
		})
	}
}
