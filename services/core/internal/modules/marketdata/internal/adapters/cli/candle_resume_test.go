package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "omni-folio/services/core/internal/providers/kiwoom"
)

func TestResumeCandleCaptureReplaysPrefixAndPublishesOnce(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "capture.json")
	args := append(captureArgs(path, "1d"), "-work", work)
	var phase atomic.Int32
	oldRow := strings.Replace(captureDailyRow, "20260824", "20260821", 1)
	transport, calls := candleCaptureTLS(t, func(w http.ResponseWriter, r *http.Request) {
		var request sdk.MockCandleRequestBody
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.BaseDate != "20300101" {
			t.Error("capture changed the original KST base date")
		}
		if r.Header.Get("next-key") == "" {
			rows, key := captureDailyRow, "old-session-cursor"
			if phase.Load() > 0 {
				key = "fresh-session-cursor"
			}
			if phase.Load() == 1 {
				rows += "," + captureDailyRow
			}
			w.Header().Set("cont-yn", "Y")
			w.Header().Set("next-key", key)
			io.WriteString(w, candleDailyBody(rows))
			return
		}
		if phase.Load() == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if phase.Load() != 2 || r.Header.Get("next-key") != "fresh-session-cursor" {
			t.Error("continued without verifying prefix or used stale cursor")
		}
		io.WriteString(w, candleDailyBody(captureDailyRow+","+oldRow))
	})
	var receipt bytes.Buffer
	conf := Config{ReadSecret: func(context.Context) ([]byte, error) { return mockSyncSecret("9876543210"), nil }, Transport: transport, Now: mockSyncNow, Pacer: mockSyncPacer()}
	run := func() error { return Run(context.Background(), "kiwoom-mock-candles-resume", args, conf, &receipt) }
	if err := run(); err == nil || calls.Load() != 3 {
		t.Fatalf("expected failure after first durable page; calls=%d err=%v", calls.Load(), err)
	}
	assertCaptureAbsent(t, path)
	phase.Store(1)
	conf.Now = func() time.Time { return mockSyncNow().Add(24 * time.Hour) }
	if err := run(); err == nil || !strings.Contains(err.Error(), "prefix") || calls.Load() != 5 {
		t.Fatalf("changed duplicate count must stop before next page; calls=%d err=%v", calls.Load(), err)
	}
	assertCaptureAbsent(t, path)
	phase.Store(2)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	capture, artifact := readCapture(t, path)
	if len(capture.Bars) != 2 || capture.Bars[0].ProviderTime != "20260821" || capture.Bars[1].ProviderTime != "20260824" || capture.Request.Body.BaseDate != "20300101" || capture.FetchStartedAt != mockSyncNow().Format(time.RFC3339Nano) || capture.FetchedAt != conf.Now().Format(time.RFC3339Nano) || calls.Load() != 8 {
		t.Fatalf("incorrect resumed capture or call count: %+v calls=%d", capture, calls.Load())
	}
	assertCaptureReceipt(t, receipt.Bytes(), artifact, 2)
	conf.ReadSecret = func(context.Context) ([]byte, error) { t.Fatal("sealed journal read credentials"); return nil, nil }
	if err := run(); err == nil {
		t.Fatal("overwrote published output")
	}
	args[5] = filepath.Join(dir, "recovered.json")
	receipt.Reset()
	if err := run(); err != nil {
		t.Fatal(err)
	}
	_, recovered := readCapture(t, args[5])
	if !bytes.Equal(artifact, recovered) || calls.Load() != 8 {
		t.Fatal("terminal recovery changed artifact or used network")
	}
	assertCaptureReceipt(t, receipt.Bytes(), recovered, 2)
	files, err := os.ReadDir(work)
	if err != nil || len(files) == 0 {
		t.Fatalf("missing checkpoint: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(work, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"old-session-cursor", "fresh-session-cursor", "synthetic-token", "synthetic-app-key", "synthetic-secret-key", "9876543210"} {
			if bytes.Contains(data, []byte(marker)) {
				t.Fatalf("private checkpoint persisted forbidden marker %q", marker)
			}
		}
	}
}

func TestResumeCandleOutputCannotUseCheckpointDirectory(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "Work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(work, alias); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(work, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{work, alias, child}
	if _, err := os.Stat(filepath.Join(dir, "work")); err == nil {
		paths = append(paths, filepath.Join(dir, "work"))
	}
	for _, path := range paths {
		args := append(captureArgs(filepath.Join(path, "candle-checkpoint.sqlite-journal"), "1d"), "-work", work)
		err := Run(context.Background(), "kiwoom-mock-candles-resume", args, Config{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("path %q might overwrite a checkpoint sidecar: %v", path, err)
		}
	}
}
