package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	adapter "omni-folio/services/core/internal/modules/marketdata/internal/adapters/provider"
	journals "omni-folio/services/core/internal/modules/marketdata/internal/adapters/sqlite"
	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
)

// Compare directories by identity so a case or symlink alias cannot publish over a SQLite sidecar.
func checkCaptureOutput(work, parent string) error {
	invalid := errors.New("candle output must be outside an accessible checkpoint directory")
	checkpointDir, err := os.Stat(work)
	if err != nil || !checkpointDir.IsDir() {
		return invalid
	}
	for {
		dir, err := os.Stat(parent)
		if err != nil || os.SameFile(checkpointDir, dir) {
			return invalid
		}
		next := filepath.Dir(parent)
		if next == parent {
			return nil
		}
		parent = next
	}
}

func resumeCandles(ctx context.Context, work, symbol, interval string, config adapter.Config) (result kiwoomCandleCapture, err error) {
	journal, err := journals.OpenCandleCheckpoint(ctx, work, symbol, interval)
	if err != nil {
		return result, errors.New("private candle checkpoint unavailable; check directory, scope and active writer")
	}
	defer func() {
		if closeErr := journal.Close(); closeErr != nil && err == nil {
			err = errors.New("private candle checkpoint close failed")
		}
	}()
	raw, err := application.ResumeCandleCapture(ctx, journal, config.Now, func(start time.Time, accept func(application.CandleCheckpointPage) error) error {
		_, fetchErr := adapter.CapturePages(ctx, symbol, interval, config, start, func(page sdk.MockCandlePage) error {
			capture, err := describeCandleCapture(symbol, interval, page.Batch)
			if err != nil {
				return err
			}
			payload, err := encodeCandleCapture(capture)
			if err != nil {
				return err
			}
			return accept(application.CandleCheckpointPage{Number: page.Number, Digest: page.Digest, StartedAt: page.Batch.FetchStartedAt, Complete: page.Complete, Payload: payload})
		})
		return fetchErr
	})
	if err != nil {
		if errors.Is(err, application.ErrCapturePrefixChanged) {
			return result, application.ErrCapturePrefixChanged
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("candle capture interrupted; retry with the same private work directory")
	}
	if err = json.Unmarshal(raw, &result); err != nil || result.SchemaVersion != "kiwoom-candle-capture.v1" || result.Symbol != symbol || result.Interval != interval || len(result.Bars) == 0 {
		return result, errors.New("invalid completed candle checkpoint")
	}
	return result, nil
}
