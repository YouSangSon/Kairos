package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	privatefile "omni-folio/services/core/internal/files/private"
	adapter "omni-folio/services/core/internal/modules/marketdata/internal/adapters/provider"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"strings"
	"time"
)

const maxKiwoomCandleCaptureBytes = 1 << 20

type kiwoomCandleTimeEvidence struct {
	ProviderField    string `json:"provider_field"`
	ProviderFormat   string `json:"provider_format"`
	ProviderTimezone string `json:"provider_timezone"`
	BarBoundaries    string `json:"bar_boundaries"`
	PublicationTime  string `json:"publication_time"`
	BarCompletion    string `json:"bar_completion"`
}

type kiwoomCandleCaptureBounds struct {
	BarsLimit  int    `json:"bars_limit"`
	PagesLimit int    `json:"pages_limit"`
	Selection  string `json:"selection"`
}

type kiwoomCandleCapture struct {
	SchemaVersion  string                        `json:"schema_version"`
	Scope          string                        `json:"scope"`
	Provider       string                        `json:"provider"`
	Environment    sdk.KiwoomEnvironment         `json:"environment"`
	Venue          string                        `json:"venue"`
	Currency       string                        `json:"currency"`
	Symbol         string                        `json:"symbol"`
	Interval       string                        `json:"interval"`
	Request        sdk.MockCandleRequestEvidence `json:"request"`
	FetchStartedAt string                        `json:"fetch_started_at"`
	FetchedAt      string                        `json:"fetched_at"`
	TimeEvidence   kiwoomCandleTimeEvidence      `json:"time_evidence"`
	Bounds         kiwoomCandleCaptureBounds     `json:"bounds"`
	Bars           []sdk.MockCapturedCandle      `json:"bars"`
}

type kiwoomCandleCaptureReceipt struct {
	SchemaVersion string `json:"schema_version"`
	Rows          int    `json:"rows"`
	SHA256        string `json:"sha256"`
}

func runCandles(ctx context.Context, args []string, out io.Writer, config Config) error {
	if ctx == nil {
		return errors.New("kiwoom mock candle context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("kiwoom-mock-candles", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	symbol := fs.String("symbol", "", "six-digit KRX stock code")
	interval := fs.String("interval", "", "daily or supported minute interval")
	path := fs.String("out", "", "new private research JSON")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *symbol == "" || *interval == "" || *path == "" {
		return errors.New("kiwoom-mock-candles requires only -symbol, -interval and -out")
	}
	if err := sdk.ValidateMockCandleRequest(*symbol, *interval); err != nil {
		return err
	}
	invalid := errors.New("kiwoom mock candle capture failed; existing files were not overwritten; a completed capture may remain after publication")
	if _, _, err := privatefile.Target(*path); err != nil {
		return invalid
	}
	batch, err := adapter.Capture(ctx, *symbol, *interval, adapter.Config{ReadSecret: config.ReadSecret, Transport: config.Transport, Now: config.Now, Pacer: config.Pacer})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if batch.FetchedAt.Before(batch.FetchStartedAt) {
		return invalid
	}
	timeEvidence := kiwoomCandleTimeEvidence{
		ProviderField: "dt", ProviderFormat: "YYYYMMDD", ProviderTimezone: "not_provided",
		BarBoundaries: "not_provided", PublicationTime: "not_provided", BarCompletion: "unverified",
	}
	rawTimeDigits := 8
	if *interval != "1d" {
		timeEvidence.ProviderField, timeEvidence.ProviderFormat = "cntr_tm", "YYYYMMDDHHmmss"
		rawTimeDigits = 14
	}
	for _, bar := range batch.Bars {
		if len(bar.ProviderTime) != rawTimeDigits || strings.Trim(bar.ProviderTime, "0123456789") != "" {
			return invalid
		}
	}
	capture := kiwoomCandleCapture{
		SchemaVersion: "kiwoom-candle-capture.v1", Scope: "research_observation", Provider: "kiwoom", Environment: sdk.KiwoomMock,
		Venue: "XKRX", Currency: "KRW", Symbol: *symbol, Interval: *interval, Request: batch.Request,
		FetchStartedAt: batch.FetchStartedAt.Format(time.RFC3339Nano), FetchedAt: batch.FetchedAt.Format(time.RFC3339Nano),
		TimeEvidence: timeEvidence, Bounds: kiwoomCandleCaptureBounds{500, 32, "latest_up_to_limit_after_overlap_validation"}, Bars: batch.Bars,
	}
	digest, err := publishKiwoomCandleCapture(ctx, *path, capture)
	if err != nil {
		return errors.Join(ctx.Err(), invalid)
	}
	if err := json.NewEncoder(out).Encode(kiwoomCandleCaptureReceipt{SchemaVersion: "kiwoom-candle-capture-receipt.v1", Rows: len(batch.Bars), SHA256: digest}); err != nil {
		return invalid
	}
	return nil
}

func publishKiwoomCandleCapture(ctx context.Context, out string, capture kiwoomCandleCapture) (string, error) {
	parent, target, err := privatefile.Target(out)
	if err != nil {
		return "", err
	}
	return privatefile.Publish(ctx, parent, target, ".omni-kiwoom-candles-*", func(output io.Writer) error {
		raw, err := json.Marshal(capture)
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		if len(raw) > maxKiwoomCandleCaptureBytes {
			return errors.New("kiwoom candle JSON exceeds the capture size limit")
		}
		n, err := output.Write(raw)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
		return err
	})
}
