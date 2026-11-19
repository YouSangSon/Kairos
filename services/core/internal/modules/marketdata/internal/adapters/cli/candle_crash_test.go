package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "omni-folio/services/core/internal/providers/kiwoom"
)

func TestResumeCandleCaptureAfterProcessExit(t *testing.T) {
	const childDirEnv = "OMNI_TEST_CANDLE_CRASH_DIR"
	const crashExitCode = 41

	dir := os.Getenv(childDirEnv)
	child := dir != ""
	if !child {
		dir = t.TempDir()
	}
	work, path := filepath.Join(dir, "work"), filepath.Join(dir, "capture.json")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if !child {
		if err := os.Mkdir(work, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResumeCandleCaptureAfterProcessExit$")
		cmd.Env = append(os.Environ(), childDirEnv+"="+dir)
		output, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != crashExitCode {
			t.Fatalf("child did not exit while requesting page two: %v\n%s", err, output)
		}
		assertCaptureAbsent(t, path)
	}

	now, cursor := mockSyncNow, "cursor-before-exit"
	if !child {
		now = func() time.Time { return mockSyncNow().AddDate(0, 0, 1) }
		cursor = "cursor-after-exit"
	}
	var chartCalls int
	transport := kiwoomRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == kiwoomTokenPath {
			token := strings.Replace(syntheticTokenJSON("synthetic-token"), "20300102000000", "20310102000000", 1)
			return kiwoomResponse(http.StatusOK, nil, token), nil
		}
		var request sdk.MockCandleRequestBody
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.BaseDate != "20300101" || r.URL.Path != kiwoomChartPath {
			t.Fatalf("capture did not request the original KST base date: %+v (%v)", request, err)
		}
		chartCalls++
		headers := map[string]string{"api-id": "ka10081"}
		key := r.Header.Get("next-key")
		if chartCalls == 1 && key == "" {
			headers["cont-yn"], headers["next-key"] = "Y", cursor
			return kiwoomResponse(http.StatusOK, headers, candleDailyBody(captureDailyRow)), nil
		}
		if chartCalls != 2 || key != cursor || r.Header.Get("cont-yn") != "Y" {
			t.Fatalf("page %d used cursor %q, want replay then %q", chartCalls, key, cursor)
		}
		if child {
			// Leave the committed first page without unwinding the journal or provider defers.
			os.Exit(crashExitCode)
		}
		older := strings.Replace(captureDailyRow, "20260824", "20260821", 1)
		return kiwoomResponse(http.StatusOK, headers, candleDailyBody(older)), nil
	})
	config := Config{
		ReadSecret: func(context.Context) ([]byte, error) { return mockSyncSecret("9876543210"), nil },
		Transport:  transport,
		Now:        now,
		Pacer:      mockSyncPacer(),
	}
	args := append(captureArgs(path, "1d"), "-work", work)
	run := func() error { return Run(ctx, "kiwoom-mock-candles-resume", args, config, io.Discard) }
	if err := run(); child || err != nil {
		t.Fatalf("capture returned unexpectedly: child=%t err=%v", child, err)
	}
	capture, raw := readCapture(t, path)
	if chartCalls != 2 || len(capture.Bars) != 2 || capture.Bars[0].ProviderTime != "20260821" || capture.Bars[1].ProviderTime != "20260824" ||
		capture.FetchStartedAt != mockSyncNow().Format(time.RFC3339Nano) || capture.FetchedAt != now().Format(time.RFC3339Nano) {
		t.Fatalf("resumed artifact is missing the original page or fetch clocks: %+v", capture)
	}
	if err := run(); err == nil {
		t.Fatal("retry accepted the previously published output path")
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("retry changed the published output: %v", err)
	}
}
