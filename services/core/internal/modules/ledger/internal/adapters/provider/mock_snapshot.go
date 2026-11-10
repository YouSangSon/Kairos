package provider

import (
	"context"
	"net/http"
	app "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/internal/providers/kiwoom"
	"time"
)

type MockSnapshots struct {
	ReadSecret func(context.Context) ([]byte, error)
	Transport  http.RoundTripper
	Now        func() time.Time
	Pacer      *kiwoom.MockReadPacer
}

func (f MockSnapshots) Open(ctx context.Context) (app.MockSnapshot, error) {
	read := f.ReadSecret
	if read == nil {
		read = kiwoom.ReadMockSecret
	}
	raw, err := read(ctx)
	defer clear(raw)
	if err != nil {
		return nil, app.MockSyncFailure(ctx, "Keychain record is unavailable")
	}
	credentials, key, err := kiwoom.DecodeMockCredentials(raw)
	defer clear(key)
	if err != nil {
		return nil, err
	}
	reader, err := kiwoom.NewMockSnapshot(kiwoom.MockSnapshotConfig{AppKey: credentials.AppKey, SecretKey: credentials.SecretKey, AccountNumber: credentials.AccountNumber, AliasKey: key, Transport: f.Transport, Now: f.Now, Pacer: f.Pacer})
	if err != nil {
		return nil, app.MockSyncFailure(ctx, "credential record is invalid")
	}
	return reader, nil
}
