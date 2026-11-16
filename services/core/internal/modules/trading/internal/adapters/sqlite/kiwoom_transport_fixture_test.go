package sqlite

import (
	"encoding/json"
	"io"
	"net/http"
	provider "omni-folio/services/core/internal/providers/kiwoom"
	"strings"
	"sync"
	"testing"
)

type kiwoomRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn kiwoomRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type kiwoomScript struct {
	t     *testing.T
	mu    sync.Mutex
	steps []func(*http.Request) *http.Response
	calls int
}

func (script *kiwoomScript) RoundTrip(request *http.Request) (*http.Response, error) {
	script.mu.Lock()
	defer script.mu.Unlock()
	if script.calls >= len(script.steps) {
		script.t.Fatalf("unexpected Kiwoom request %d: %s", script.calls+1, request.URL.Path)
	}
	step := script.steps[script.calls]
	script.calls++
	return step(request), nil
}

func syntheticTokenJSON(token string) string {
	return `{"expires_dt":"20300102000000","token_type":"Bearer","token":"` + token + `","return_code":0,"return_msg":"synthetic token message"}`
}
func kiwoomResponse(status int, headers map[string]string, body string) *http.Response {
	header := make(http.Header)
	for key, value := range headers {
		header.Set(key, value)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}
func assertKiwoomAuthorization(t *testing.T, request *http.Request, token string) {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer "+token {
		t.Fatalf("unexpected authorization header")
	}
}
func assertNoKiwoomCursor(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("cont-yn") != "" || request.Header.Get("next-key") != "" {
		t.Fatalf("unexpected initial cursor headers")
	}
}
func decodeKiwoomRequest(t *testing.T, request *http.Request, destination any) {
	t.Helper()
	defer request.Body.Close()
	if err := json.NewDecoder(request.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
func newSyntheticKiwoomClient(t *testing.T, environment KiwoomEnvironment, transport http.RoundTripper) *KiwoomClient {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	client, err := provider.NewClient(environment, "synthetic-app-key", "synthetic-secret-key", []byte("0123456789abcdef0123456789abcdef"))
	http.DefaultTransport = previous
	if err != nil {
		t.Fatal(err)
	}
	return client
}
