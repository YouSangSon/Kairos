package provider

import (
	"context"
	"errors"
	"net/http"
	"time"

	sdk "omni-folio/services/core/internal/providers/kiwoom"
)

type Config struct {
	ReadSecret func(context.Context) ([]byte, error)
	Transport  http.RoundTripper
	Now        func() time.Time
	Pacer      *sdk.MockReadPacer
}

var errCandleCapture = errors.New("kiwoom mock candle capture failed; existing files were not overwritten; a completed capture may remain after publication")

func Capture(ctx context.Context, symbol, interval string, config Config) (sdk.MockCandleBatch, error) {
	return capture(ctx, symbol, interval, config, time.Time{}, nil)
}

func CapturePages(ctx context.Context, symbol, interval string, config Config, startedAt time.Time, visit func(sdk.MockCandlePage) error) (sdk.MockCandleBatch, error) {
	if startedAt.IsZero() || visit == nil {
		return sdk.MockCandleBatch{}, errCandleCapture
	}
	return capture(ctx, symbol, interval, config, startedAt, visit)
}

func capture(ctx context.Context, symbol, interval string, config Config, startedAt time.Time, visit func(sdk.MockCandlePage) error) (sdk.MockCandleBatch, error) {
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
		return sdk.MockCandleBatch{}, errCandleCapture
	}
	defer client.Close()
	var batch sdk.MockCandleBatch
	if visit == nil {
		batch, err = client.Capture(ctx, symbol, interval)
	} else {
		var visitErr error
		batch, err = client.CapturePages(ctx, symbol, interval, startedAt, func(page sdk.MockCandlePage) error {
			visitErr = visit(page)
			return visitErr
		})
		if visitErr != nil {
			return sdk.MockCandleBatch{}, visitErr
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return sdk.MockCandleBatch{}, ctx.Err()
		}
		return sdk.MockCandleBatch{}, errCandleCapture
	}
	return batch, nil
}
