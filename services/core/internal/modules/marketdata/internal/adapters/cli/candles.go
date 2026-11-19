package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	privatefile "omni-folio/services/core/internal/files/private"
	adapter "omni-folio/services/core/internal/modules/marketdata/internal/adapters/provider"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
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

func runCandles(ctx context.Context, command string, args []string, out io.Writer, config Config) error {
	resume := command == "kiwoom-mock-candles-resume"
	if ctx == nil {
		return errors.New("kiwoom mock candle context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	symbol := fs.String("symbol", "", "six-digit KRX stock code")
	interval := fs.String("interval", "", "daily or supported minute interval")
	path := fs.String("out", "", "new private research JSON")
	var work string
	if resume {
		fs.StringVar(&work, "work", "", "private capture checkpoint directory")
	}
	if fs.Parse(args) != nil || fs.NArg() != 0 || *symbol == "" || *interval == "" || *path == "" || (resume && work == "") {
		if resume {
			return errors.New("kiwoom-mock-candles-resume requires -symbol, -interval, -work and -out")
		}
		return errors.New("kiwoom-mock-candles requires only -symbol, -interval and -out")
	}
	if err := sdk.ValidateMockCandleRequest(*symbol, *interval); err != nil {
		return err
	}
	invalid := errors.New("kiwoom mock candle capture failed; existing files were not overwritten; a completed capture may remain after publication")
	parent, target, err := privatefile.Target(*path)
	if err != nil {
		return invalid
	}
	provider := adapter.Config{ReadSecret: config.ReadSecret, Transport: config.Transport, Now: config.Now, Pacer: config.Pacer}
	var capture kiwoomCandleCapture
	if resume {
		if err := checkCaptureOutput(work, parent); err != nil {
			return err
		}
		capture, err = resumeCandles(ctx, work, *symbol, *interval, provider)
		if err != nil {
			return err
		}
	} else {
		batch, err := adapter.Capture(ctx, *symbol, *interval, provider)
		if err != nil {
			return err
		}
		capture, err = describeCandleCapture(*symbol, *interval, batch)
		if err != nil {
			return invalid
		}
	}
	digest, err := publishKiwoomCandleCapture(ctx, target, capture)
	if err != nil {
		return errors.Join(ctx.Err(), invalid)
	}
	if err := json.NewEncoder(out).Encode(kiwoomCandleCaptureReceipt{SchemaVersion: "kiwoom-candle-capture-receipt.v1", Rows: len(capture.Bars), SHA256: digest}); err != nil {
		return invalid
	}
	return nil
}

func describeCandleCapture(symbol, interval string, batch sdk.MockCandleBatch) (kiwoomCandleCapture, error) {
	invalid := errors.New("invalid candle observations or fetch times")
	if batch.FetchedAt.Before(batch.FetchStartedAt) {
		return kiwoomCandleCapture{}, invalid
	}
	timeEvidence := kiwoomCandleTimeEvidence{
		ProviderField: "dt", ProviderFormat: "YYYYMMDD", ProviderTimezone: "not_provided",
		BarBoundaries: "not_provided", PublicationTime: "not_provided", BarCompletion: "unverified",
	}
	rawTimeDigits := 8
	if interval != "1d" {
		timeEvidence.ProviderField, timeEvidence.ProviderFormat = "cntr_tm", "YYYYMMDDHHmmss"
		rawTimeDigits = 14
	}
	for _, bar := range batch.Bars {
		if len(bar.ProviderTime) != rawTimeDigits || strings.Trim(bar.ProviderTime, "0123456789") != "" {
			return kiwoomCandleCapture{}, invalid
		}
	}
	return kiwoomCandleCapture{
		SchemaVersion: "kiwoom-candle-capture.v1", Scope: "research_observation", Provider: "kiwoom", Environment: sdk.KiwoomMock,
		Venue: "XKRX", Currency: "KRW", Symbol: symbol, Interval: interval, Request: batch.Request,
		FetchStartedAt: batch.FetchStartedAt.Format(time.RFC3339Nano), FetchedAt: batch.FetchedAt.Format(time.RFC3339Nano),
		TimeEvidence: timeEvidence, Bounds: kiwoomCandleCaptureBounds{500, 32, "latest_up_to_limit_after_overlap_validation"}, Bars: batch.Bars,
	}, nil
}

func encodeCandleCapture(capture kiwoomCandleCapture) ([]byte, error) {
	raw, err := json.Marshal(capture)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > maxKiwoomCandleCaptureBytes {
		return nil, errors.New("kiwoom candle JSON exceeds the capture size limit")
	}
	return raw, nil
}

func publishKiwoomCandleCapture(ctx context.Context, out string, capture kiwoomCandleCapture) (string, error) {
	parent, target, err := privatefile.Target(out)
	if err != nil {
		return "", err
	}
	return privatefile.Publish(ctx, parent, target, ".omni-kiwoom-candles-*", func(output io.Writer) error {
		raw, err := encodeCandleCapture(capture)
		if err != nil {
			return err
		}
		n, err := output.Write(raw)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
		return err
	})
}
