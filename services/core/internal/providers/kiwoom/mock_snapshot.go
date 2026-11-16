package kiwoom

import (
	"context"
	"net/http"
	"time"
)

type MockSnapshotConfig struct {
	AppKey, SecretKey, AccountNumber string
	AliasKey                         []byte
	Transport                        http.RoundTripper
	Now                              func() time.Time
	Pacer                            *MockReadPacer
}
type MockSnapshot struct {
	client   *Client
	expected string
}

func NewMockSnapshot(config MockSnapshotConfig) (*MockSnapshot, error) {
	if !kiwoomAccountPattern.MatchString(config.AccountNumber) || config.Transport == nil || config.Now == nil || !config.Pacer.valid() {
		return nil, &KiwoomError{Kind: "invalid_config"}
	}
	client, err := NewClient(KiwoomMock, config.AppKey, config.SecretKey, config.AliasKey)
	if err != nil {
		return nil, err
	}
	client.now = config.Now
	client.transport = &mockSnapshotTransport{base: config.Transport, pacer: config.Pacer}
	return &MockSnapshot{client: client, expected: client.alias("account", config.AccountNumber)}, nil
}
func (s *MockSnapshot) AccountRef() string {
	if s == nil || s.client == nil {
		return ""
	}
	return s.expected
}
func (s *MockSnapshot) Snapshot(ctx context.Context) (*KiwoomSnapshot, error) {
	if s == nil || s.client == nil || s.client.environment != KiwoomMock {
		return nil, &KiwoomError{Kind: "invalid_config"}
	}
	return s.client.Snapshot(ctx, KiwoomKRX)
}
func (s *MockSnapshot) Close() {
	if s == nil || s.client == nil {
		return
	}
	clear(s.client.aliasKey)
	s.client.aliasKey = nil
	s.client.appKey = ""
	s.client.secretKey = ""
	s.client.tokenMu.Lock()
	s.client.accessToken = ""
	s.client.tokenExpiresAt = time.Time{}
	s.client.tokenMu.Unlock()
	s.client = nil
	s.expected = ""
}

type mockSnapshotTransport struct {
	base  http.RoundTripper
	pacer *MockReadPacer
}

func (t *mockSnapshotTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.Scheme != "https" || request.URL.Host != "mockapi.kiwoom.com" || request.URL.RawQuery != "" || request.URL.User != nil ||
		!((request.URL.Path == kiwoomTokenPath && request.Header.Get("api-id") == "") || (request.URL.Path == kiwoomAccountPath && (request.Header.Get("api-id") == "ka00001" || request.Header.Get("api-id") == "kt00018" || request.Header.Get("api-id") == "ka10075"))) {
		return nil, &KiwoomError{Kind: "invalid_request"}
	}
	return t.pacer.roundTrip(request, t.base)
}
