package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"omni-folio/services/core/internal/composition"
	market "omni-folio/services/core/internal/modules/marketdata"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const modularDailyRow = `{"dt":"20260824","cur_prc":"-001,250.00","open_pric":"+001200","high_pric":"1300","low_pric":"-1150","trde_qty":"+001000"}`
const modularTokenPath = "/oauth2/token"
const modularChartPath = "/api/dostk/chart"

type modularRoundTripFunc func(*http.Request) (*http.Response, error)

func (f modularRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func modularTokenJSON(token string) string {
	return `{"expires_dt":"20300102000000","token_type":"Bearer","token":"` + token + `","return_code":0}`
}
func modularNow() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
func modularPacer() *sdk.MockReadPacer {
	clock := modularNow()
	return sdk.NewMockReadPacer(func() time.Time { return clock }, func(ctx context.Context, delay time.Duration) error { clock = clock.Add(delay); return ctx.Err() })
}
func modularSecret() []byte {
	raw, _ := json.Marshal(sdk.MockCredentials{SchemaVersion: "kiwoom-mock-read.v1", AppKey: "synthetic-app-key", SecretKey: "synthetic-secret-key", AliasKeyBase64: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), AccountNumber: "9876543210"})
	return raw
}
func modularCaptureArgs(path string) []string {
	return []string{"-symbol", "005930", "-interval", "1d", "-out", path}
}
func TestModularCandlesMainLocalTLSAndSignals(t *testing.T) {
	for _, mode := range []string{"success", "interrupt", "terminate"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.json")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestModularCandlesMainHelper$")
			cmd.Env = append(os.Environ(), "OMNI_TEST_CANDLE_MAIN="+mode, "OMNI_TEST_CANDLE_OUT="+path)
			var output bytes.Buffer
			cmd.Stdout = &output
			if mode == "success" {
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("controlled actual main failed: %v %s", err, stderr.Bytes())
				}
				_, raw := modularReadCapture(t, path)
				fixture, err := os.ReadFile("../../contracts/fixtures/kiwoom-candle-capture.json")
				if err != nil {
					t.Fatal(err)
				}
				var actual, expected any
				if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(fixture, &expected) != nil || !reflect.DeepEqual(actual, expected) {
					t.Fatal("actual main capture differs from the versioned closed JSON fixture")
				}
				line, _, _ := bytes.Cut(output.Bytes(), []byte("\n"))
				modularAssertReceipt(t, line, raw, 1)
				return
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(stderr)
			if !scanner.Scan() || scanner.Text() != "candle-main-awaiting-signal" {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatal("actual main never reached the cancellable reader")
			}
			signal := os.Interrupt
			if mode == "terminate" {
				signal = syscall.SIGTERM
			}
			if err := cmd.Process.Signal(signal); err != nil {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatal(err)
			}
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			err = cmd.Wait()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || ctx.Err() != nil || !strings.Contains(strings.Join(lines, "\n"), "context canceled") || output.Len() != 0 {
				t.Fatalf("actual main did not cancel normally after %s: %v %v", mode, err, lines)
			}
			modularAssertAbsent(t, path)
		})
	}
}

func TestModularCandlesMainHelper(t *testing.T) {
	mode := os.Getenv("OMNI_TEST_CANDLE_MAIN")
	if mode == "" {
		return
	}
	closed := false
	config := market.CLIConfig{Now: modularNow, Pacer: modularPacer()}
	if mode == "success" {
		transport, _ := modularCandleTLS(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"stk_cd":"005930","stk_dt_pole_chart_qry":[`+modularDailyRow+`],"return_code":0}`)
		})
		config.ReadSecret = func(context.Context) ([]byte, error) { return modularSecret(), nil }
		config.Transport = transport
	} else {
		config.ReadSecret = func(ctx context.Context) ([]byte, error) {
			fmt.Fprintln(os.Stderr, "candle-main-awaiting-signal")
			<-ctx.Done()
			return nil, ctx.Err()
		}
		config.Transport = modularRoundTripFunc(func(*http.Request) (*http.Response, error) { panic("signal test reached network") })
	}
	cliOptions = composition.CLIOptions{MockReadRuntime: func(composition.Resources) composition.MockReadRuntime {
		return composition.MockReadRuntime{Market: config, Close: func() { closed = true }}
	}}
	os.Args = append([]string{"omni-core", "kiwoom-mock-candles"}, modularCaptureArgs(os.Getenv("OMNI_TEST_CANDLE_OUT"))...)
	main()
	if !closed {
		t.Fatal("command runtime was not closed")
	}
}

type modularCandleTimeEvidence struct {
	ProviderField    string `json:"provider_field"`
	ProviderFormat   string `json:"provider_format"`
	ProviderTimezone string `json:"provider_timezone"`
	BarBoundaries    string `json:"bar_boundaries"`
	PublicationTime  string `json:"publication_time"`
	BarCompletion    string `json:"bar_completion"`
}

type modularCandleCaptureBounds struct {
	BarsLimit  int    `json:"bars_limit"`
	PagesLimit int    `json:"pages_limit"`
	Selection  string `json:"selection"`
}

type modularCaptureDocument struct {
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
	TimeEvidence   modularCandleTimeEvidence     `json:"time_evidence"`
	Bounds         modularCandleCaptureBounds    `json:"bounds"`
	Bars           []sdk.MockCapturedCandle      `json:"bars"`
}

func modularReadCapture(t *testing.T, path string) (modularCaptureDocument, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var capture modularCaptureDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&capture) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("published capture is not closed typed JSON")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("capture output is not private mode 0600")
	}
	return capture, raw
}
func modularAssertReceipt(t *testing.T, output, raw []byte, rows int) {
	t.Helper()
	var receipt struct {
		SchemaVersion string `json:"schema_version"`
		Rows          int    `json:"rows"`
		SHA256        string `json:"sha256"`
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatalf("receipt is not closed JSON: %s %v", output, err)
	}
	digest := sha256.Sum256(raw)
	if receipt.SchemaVersion != "kiwoom-candle-capture-receipt.v1" || receipt.Rows != rows || receipt.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("receipt does not identify exact published bytes")
	}
}
func modularAssertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed capture published a final file: %v", err)
	}
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".omni-kiwoom-candles-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("owned capture temp remains: %v %v", temporary, err)
	}
}
func modularCandleTLS(t *testing.T, chart http.HandlerFunc) (http.RoundTripper, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		api := r.Header.Get("api-id")
		if r.Method != http.MethodPost {
			t.Error("unexpected provider method")
		}
		if r.URL.Path == modularTokenPath && api == "" {
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]string{"grant_type": "client_credentials", "appkey": "synthetic-app-key", "secretkey": "synthetic-secret-key"}) {
				t.Error("synthetic OAuth body differs")
			}
			io.WriteString(w, strings.Replace(modularTokenJSON("synthetic-token"), "20300102000000", "20310102000000", 1))
			return
		}
		if r.URL.Path != modularChartPath || (api != "ka10080" && api != "ka10081") || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("unexpected chart capability or authorization")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("api-id", api)
		chart(w, r)
	}))
	t.Cleanup(server.Close)
	base := server.Client().Transport.(*http.Transport)
	t.Cleanup(base.CloseIdleConnections)
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return modularRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "mockapi.kiwoom.com" {
			return nil, errors.New("test transport refuses non-mock origin")
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host, request.Host = local.Scheme, local.Host, local.Host
		return base.RoundTrip(request)
	}), calls
}
