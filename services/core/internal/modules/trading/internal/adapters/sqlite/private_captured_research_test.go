package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	market "omni-folio/services/core/internal/modules/marketdata"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapturedCandleResearchActualProducerAndPythonCannotEnterPaperAuthority(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(filepath.Dir(fixturePath("strategy-market-bars.csv")), "../.."))
	if err != nil {
		t.Fatal(err)
	}
	rawCSV, err := os.ReadFile(filepath.Join(root, "contracts/fixtures/strategy-market-bars.csv"))
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(rawCSV)).ReadAll()
	if err != nil || len(records) != 121 {
		t.Fatalf("synthetic research CSV: %d rows, %v", len(records)-1, err)
	}
	rows := make([]map[string]string, 0, len(records)-1)
	for index := len(records) - 1; index > 0; index-- {
		row := records[index]
		rows = append(rows, map[string]string{
			"dt": strings.ReplaceAll(row[0][:10], "-", ""), "open_pric": row[2], "high_pric": row[3],
			"low_pric": row[4], "cur_prc": row[5], "trde_qty": row[6],
		})
	}
	transport, calls := capturedModularCandleTLS(t, func(w http.ResponseWriter, r *http.Request) {
		var body sdk.MockCandleRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Header.Get("api-id") != "ka10081" || body.StockCode != "005930" || body.BaseDate != "20300101" {
			t.Errorf("synthetic capture request: %+v, %v", body, err)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"stk_cd": "005930", "stk_dt_pole_chart_qry": rows, "return_code": 0}); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(t.TempDir(), "synthetic-capture.json")
	var captureReceipt bytes.Buffer
	if err := market.RunCLI(context.Background(), "kiwoom-mock-candles", capturedModularCaptureArgs(path), market.CLIConfig{ReadSecret: func(context.Context) ([]byte, error) { return capturedModularSecret(), nil }, Transport: transport, Now: capturedModularNow, Pacer: capturedModularPacer()}, &captureReceipt); err != nil {
		t.Fatal(err)
	}

	capture, rawCapture := capturedModularReadCapture(t, path)
	if len(capture.Bars) != 120 || calls.Load() != 2 {
		t.Fatalf("actual producer did not retain 120 synthetic observations: %d rows, %d calls", len(capture.Bars), calls.Load())
	}
	capturedModularAssertReceipt(t, captureReceipt.Bytes(), rawCapture, 120)
	command := exec.Command("python3", "-B", "-m", "omni_research.capture_cli", "--capture", path,
		"--config", filepath.Join(root, "contracts/fixtures/strategy-improvement-paper-config.json"))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(root, "services/research"))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	rawReport, err := command.Output()
	if err != nil || stderr.Len() != 0 {
		t.Fatalf("actual Python capture CLI: %v %s", err, stderr.String())
	}
	if len(rawReport) == 0 || len(rawReport) >= maxBodyBytes || !json.Valid(rawReport) {
		t.Fatalf("report must be valid JSON below the Go body limit: %d bytes", len(rawReport))
	}
	var report struct {
		SchemaVersion string `json:"schema_version"`
		Source        struct {
			SHA256  string                         `json:"sha256"`
			Capture capturedModularCaptureDocument `json:"capture"`
		} `json:"source"`
		Analysis struct {
			Comparisons map[string]bool `json:"threshold_comparisons"`
		} `json:"analysis"`
	}
	if err := json.Unmarshal(rawReport, &report); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(rawCapture)
	if report.SchemaVersion != "captured-candle-research.v1" || report.Source.SHA256 != hex.EncodeToString(sum[:]) || len(report.Source.Capture.Bars) != 120 {
		t.Fatal("Python report lost the native capture provenance")
	}
	if len(report.Analysis.Comparisons) != 3 {
		t.Fatal("report did not compute all research threshold comparisons")
	}
	for _, passed := range report.Analysis.Comparisons {
		if !passed {
			t.Fatal("synthetic fixture must pass numerical thresholds before authority refusal")
		}
	}
	if _, err := decodeStrategyArtifact(rawReport); err == nil {
		t.Fatal("capture report passed the strategy artifact decoder")
	}
	if _, err := decodePaperProposal(rawReport); err == nil {
		t.Fatal("capture report passed the paper proposal decoder")
	}
	svc, _ := testService(t, nil, nil)
	forward := paperPromotionForwardBars(t, mustTime("2030-02-01T00:00:00Z"), 24*time.Hour)
	if _, _, err := parsePaperPromotionBars(context.Background(), forward); err != nil {
		t.Fatalf("valid forward bars required before refusal: %v", err)
	}
	const attempt = "captured-research-refusal"
	if !orderAlias(k2aAccountRef, "account") || !safeOrderID(attempt) {
		t.Fatal("valid identifiers required before refusal")
	}
	before := paperImprovementStateForTest(t, svc)
	if _, err := svc.registerStrategyEvidence(context.Background(), rawReport); err == nil {
		t.Fatal("capture report registered durable research evidence")
	}
	if after := paperImprovementStateForTest(t, svc); after != before {
		t.Fatalf("registration refusal changed full state: before=%+v after=%+v", before, after)
	}
	if _, err := svc.ImprovePaperCandidate(context.Background(), k2aAccountRef, attempt, rawReport, forward); err == nil {
		t.Fatal("capture report reached paper improvement")
	}
	if after := paperImprovementStateForTest(t, svc); after != before {
		t.Fatalf("improvement refusal changed full state: before=%+v after=%+v", before, after)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, rawCapture) {
		t.Fatal("research changed the original producer capture")
	}
}

const capturedModularTokenPath = "/oauth2/token"

const capturedModularChartPath = "/api/dostk/chart"

type capturedModularRoundTripFunc func(*http.Request) (*http.Response, error)

type capturedModularCandleTimeEvidence struct {
	ProviderField    string `json:"provider_field"`
	ProviderFormat   string `json:"provider_format"`
	ProviderTimezone string `json:"provider_timezone"`
	BarBoundaries    string `json:"bar_boundaries"`
	PublicationTime  string `json:"publication_time"`
	BarCompletion    string `json:"bar_completion"`
}

type capturedModularCandleCaptureBounds struct {
	BarsLimit  int    `json:"bars_limit"`
	PagesLimit int    `json:"pages_limit"`
	Selection  string `json:"selection"`
}

type capturedModularCaptureDocument struct {
	SchemaVersion  string                             `json:"schema_version"`
	Scope          string                             `json:"scope"`
	Provider       string                             `json:"provider"`
	Environment    sdk.KiwoomEnvironment              `json:"environment"`
	Venue          string                             `json:"venue"`
	Currency       string                             `json:"currency"`
	Symbol         string                             `json:"symbol"`
	Interval       string                             `json:"interval"`
	Request        sdk.MockCandleRequestEvidence      `json:"request"`
	FetchStartedAt string                             `json:"fetch_started_at"`
	FetchedAt      string                             `json:"fetched_at"`
	TimeEvidence   capturedModularCandleTimeEvidence  `json:"time_evidence"`
	Bounds         capturedModularCandleCaptureBounds `json:"bounds"`
	Bars           []sdk.MockCapturedCandle           `json:"bars"`
}

func (f capturedModularRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func capturedModularTokenJSON(token string) string {
	return `{"expires_dt":"20300102000000","token_type":"Bearer","token":"` + token + `","return_code":0}`
}

func capturedModularNow() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }

func capturedModularPacer() *sdk.MockReadPacer {
	clock := capturedModularNow()
	return sdk.NewMockReadPacer(func() time.Time { return clock }, func(ctx context.Context, delay time.Duration) error { clock = clock.Add(delay); return ctx.Err() })
}

func capturedModularSecret() []byte {
	raw, _ := json.Marshal(sdk.MockCredentials{SchemaVersion: "kiwoom-mock-read.v1", AppKey: "synthetic-app-key", SecretKey: "synthetic-secret-key", AliasKeyBase64: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), AccountNumber: "9876543210"})
	return raw
}

func capturedModularCaptureArgs(path string) []string {
	return []string{"-symbol", "005930", "-interval", "1d", "-out", path}
}

func capturedModularReadCapture(t *testing.T, path string) (capturedModularCaptureDocument, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var capture capturedModularCaptureDocument
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

func capturedModularAssertReceipt(t *testing.T, output, raw []byte, rows int) {
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

func capturedModularCandleTLS(t *testing.T, chart http.HandlerFunc) (http.RoundTripper, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		api := r.Header.Get("api-id")
		if r.Method != http.MethodPost {
			t.Error("unexpected provider method")
		}
		if r.URL.Path == capturedModularTokenPath && api == "" {
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]string{"grant_type": "client_credentials", "appkey": "synthetic-app-key", "secretkey": "synthetic-secret-key"}) {
				t.Error("synthetic OAuth body differs")
			}
			io.WriteString(w, strings.Replace(capturedModularTokenJSON("synthetic-token"), "20300102000000", "20310102000000", 1))
			return
		}
		if r.URL.Path != capturedModularChartPath || (api != "ka10080" && api != "ka10081") || r.Header.Get("Authorization") != "Bearer synthetic-token" {
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
	return capturedModularRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "mockapi.kiwoom.com" {
			return nil, errors.New("test transport refuses non-mock origin")
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host, request.Host = local.Scheme, local.Host, local.Host
		return base.RoundTrip(request)
	}), calls
}
