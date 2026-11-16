package provider

import (
	"context"
	"errors"
	"net/http"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"time"
)

type Config struct {
	ReadSecret func(context.Context) ([]byte, error)
	Transport  http.RoundTripper
	Now        func() time.Time
	Pacer      *sdk.MockReadPacer
}

func Capture(ctx context.Context, symbol, interval string, config Config) (sdk.MockCandleBatch, error) {
	readSecret := config.ReadSecret
	if readSecret == nil {
		readSecret = sdk.ReadMockSecret
	}
	raw, err := readSecret(ctx)
	if err != nil {
		clear(raw)
		if ctx.Err() != nil {
			return sdk.MockCandleBatch{}, ctx.Err()
		}
		return sdk.MockCandleBatch{}, errors.New("kiwoom mock candle Keychain record is unavailable")
	}
	credentials, aliasKey, err := sdk.DecodeMockCredentials(raw)
	clear(raw)
	if err != nil {
		return sdk.MockCandleBatch{}, err
	}
	defer clear(aliasKey)
	client, err := sdk.NewMockCandles(sdk.MockCandlesConfig{AppKey: credentials.AppKey, SecretKey: credentials.SecretKey, AliasKey: aliasKey, Transport: config.Transport, Now: config.Now, Pacer: config.Pacer})
	if err != nil {
		return sdk.MockCandleBatch{}, errors.New("kiwoom mock candle capture failed; existing files were not overwritten; a completed capture may remain after publication")
	}
	defer client.Close()
	batch, err := client.Capture(ctx, symbol, interval)
	if err != nil {
		if ctx.Err() != nil {
			return sdk.MockCandleBatch{}, ctx.Err()
		}
		return sdk.MockCandleBatch{}, errors.New("kiwoom mock candle capture failed; existing files were not overwritten; a completed capture may remain after publication")
	}
	return batch, nil
}
