package research

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	c "omni-folio/services/core/internal/modules/research/contracts"
)

func TestPublicResearchNotConfigured(t *testing.T) {
	svc := newResearchService()
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed?market=US&symbol=AAPL", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "research_not_configured") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("research contract: %d %s", w.Code, w.Body.String())
	}
}

func TestPublicResearchHTTPJSONEscapingPreservesLargeValidArtifact(t *testing.T) {
	var feed map[string]any
	_ = json.Unmarshal(researchFixture(t), &feed)
	original := feed["items"].([]any)[0]
	rawOriginal, _ := json.Marshal(original)
	items := make([]any, 100)
	for i := range items {
		var item map[string]any
		_ = json.Unmarshal(rawOriginal, &item)
		item["id"] = fmt.Sprintf("%064x", i)
		item["reasons"] = []string{strings.Repeat("&", 2048)}
		items[i] = item
	}
	feed["items"] = items
	raw, _ := json.Marshal(feed)
	// The artifact budget counts UTF-8 bytes before the transport escapes HTML.
	raw = []byte(strings.ReplaceAll(string(raw), `\u0026`, "&"))
	if len(raw) >= publicResearchMaxBytes {
		t.Fatalf("test artifact over budget: %d", len(raw))
	}
	svc := newResearchService()
	svc.researchFeed = filepath.Join(t.TempDir(), "feed.json")
	if err := os.WriteFile(svc.researchFeed, raw, 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed?market=US&symbol=AAPL", nil))
	if w.Code != 200 || w.Body.Len() <= publicResearchMaxBytes+4096 {
		t.Fatalf("escaped response: %d bytes=%d", w.Code, w.Body.Len())
	}
	var view c.PublicResearchView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || len(view.Items) != 100 {
		t.Fatalf("lost response items: %v", err)
	}
	var first map[string]any
	_ = json.Unmarshal(view.Items[0], &first)
	if first["reasons"].([]any)[0] != strings.Repeat("&", 2048) {
		t.Fatal("transport changed research text")
	}
}

func researchFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../../contracts/fixtures/public-research.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPublicResearchExactIdentityEmptyAndPrivacy(t *testing.T) {
	svc := newResearchService()
	svc.researchFeed = filepath.Join(t.TempDir(), "feed.json")
	raw := researchFixture(t)
	var feed map[string]any
	if err := json.Unmarshal(raw, &feed); err != nil {
		t.Fatal(err)
	}
	items := feed["items"].([]any)
	var extra map[string]any
	copyRaw, _ := json.Marshal(items[0])
	_ = json.Unmarshal(copyRaw, &extra)
	extra["market"], extra["id"] = "KR", strings.Repeat("a", 64)
	feed["items"] = append(items, extra)
	raw, _ = json.Marshal(feed)
	if err := os.WriteFile(svc.researchFeed, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		market, symbol string
		count          int
	}{{"US", "AAPL", 3}, {"KR", "AAPL", 1}, {"US", "MSFT", 0}} {
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed?market="+tc.market+"&symbol="+tc.symbol, nil))
		var body c.PublicResearchView
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Items) != tc.count || body.Market != tc.market || body.Symbol != tc.symbol || body.SchemaVersion != "public-research-view.v1" || !body.Sample {
			t.Fatalf("identity view: %d %s", w.Code, w.Body.String())
		}
		for _, field := range []string{"idempotency_key", "account_id", "holding", "data_root", "permissions", "basis"} {
			if strings.Contains(w.Body.String(), `"`+field+`"`) {
				t.Fatalf("private field %s exposed", field)
			}
		}
	}

}

func TestPublicResearchQueryAndFileFailureBoundary(t *testing.T) {
	svc := newResearchService()
	svc.researchFeed = filepath.Join(t.TempDir(), "private-path.json")
	for _, path := range []string{
		"", "?market=US", "?market=GLOBAL&symbol=AAPL", "?market=US&symbol=aapl",
		"?market=US&symbol=AAPL&market=KR", "?market=US&symbol=AAPL&token=secret",
		"?market=US&symbol=AAPL%00", "?market=US;symbol=AAPL", "?market=%zz&symbol=AAPL",
	} {
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed"+path, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_query") {
			t.Fatalf("query %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	request := func(status int, code string) {
		t.Helper()
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed?market=US&symbol=AAPL", nil))
		if w.Code != status || !strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "private-path") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("file boundary: %d %s", w.Code, w.Body.String())
		}
	}
	request(404, "research_feed_missing")
	if err := os.WriteFile(svc.researchFeed, []byte("{private-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	request(503, "research_feed_unavailable")
	if err := os.WriteFile(svc.researchFeed, make([]byte, publicResearchMaxBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	request(503, "research_feed_unavailable")
	if err := os.Remove(svc.researchFeed); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(svc.researchFeed, 0600); err != nil {
		t.Fatal(err)
	}
	request(503, "research_feed_unavailable")
	svc.researchFeed = t.TempDir()
	request(503, "research_feed_unavailable")
}

const publicResearchMaxBytes = 1024 * 1024

type researchService struct {
	researchFeed string
	now          func() time.Time
}

func newResearchService() *researchService {
	return &researchService{now: func() time.Time { return time.Date(2026, 11, 9, 0, 0, 0, 123, time.UTC) }}
}
func (s *researchService) routes() http.Handler {
	mux := http.NewServeMux()
	RegisterHTTP(mux, Compose(Config{FeedPath: func() string { return s.researchFeed }, Now: func() time.Time { return s.now() }}))
	return mux
}

func TestPublicResearchRegistrationAndLazyInputs(t *testing.T) {
	path := ""
	clockCalls := 0
	observed := time.Date(2026, 11, 9, 10, 0, 0, 123, time.FixedZone("KST", 9*60*60))
	queries := Compose(Config{FeedPath: func() string { return path }, Now: func() time.Time { clockCalls++; return observed }})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /other", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	RegisterHTTP(mux, queries)
	request := func(method, target string, status int, body string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, target, nil))
		if w.Code != status || w.Body.String() != body {
			t.Fatalf("%s %s: %d %q", method, target, w.Code, w.Body.String())
		}
	}
	request("GET", "/v1/research/feed?market=US&symbol=MSFT", 503, "{\"code\":\"research_not_configured\",\"message\":\"public research is not configured\"}\n")
	path = filepath.Join(t.TempDir(), "feed.json")
	request("GET", "/v1/research/feed?market=US&symbol=MSFT", 404, "{\"code\":\"research_feed_missing\",\"message\":\"public research file is missing\"}\n")
	if clockCalls != 0 {
		t.Fatal("failed read observed the clock")
	}
	if err := os.WriteFile(path, researchFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	const emptyView = `{"schema_version":"public-research-view.v1","feed_schema_version":"mimir-public-research.v1","observed_at":"2026-11-09T01:00:00.000000123Z","market":"US","symbol":"MSFT","generated_at":"2026-11-03T00:00:00.000000Z","since":"2026-11-03","until":"2026-11-03","sample":true,"omitted":{"unscoped_news":0,"missing_source_evidence":0},"items":[]}` + "\n"
	request("GET", "/v1/research/feed?market=US&symbol=MSFT", 200, emptyView)
	if clockCalls != 1 {
		t.Fatalf("observed clock %d times", clockCalls)
	}
	request("GET", "/other", 204, "")
	request("GET", "/v1/research/unknown", 404, "404 page not found\n")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/research/feed", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || w.Body.String() != "Method Not Allowed\n" {
		t.Fatalf("method contract: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := server.Client().Head(server.URL + "/v1/research/feed?market=US&symbol=MSFT")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || len(body) != 0 || response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("HEAD contract: %d %v %q %v", response.StatusCode, response.Header, body, err)
	}
	// Re-read on every request; an unavailable replacement must not serve a cached feed.
	if err := os.WriteFile(path, []byte("private-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	request("GET", "/v1/research/feed?market=US&symbol=MSFT", 503, "{\"code\":\"research_feed_unavailable\",\"message\":\"public research file is unavailable\"}\n")
}

func TestPublicResearchPreservesNullableAndEmptyItems(t *testing.T) {
	svc := newResearchService()
	svc.researchFeed = filepath.Join(t.TempDir(), "feed.json")
	var feed map[string]any
	if err := json.Unmarshal(researchFixture(t), &feed); err != nil {
		t.Fatal(err)
	}
	for _, item := range feed["items"].([]any) {
		value := item.(map[string]any)
		if value["kind"] == "filing" {
			value["title"], value["summary"] = nil, nil
		}
	}
	request := func() c.PublicResearchView {
		t.Helper()
		raw, err := json.Marshal(feed)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(svc.researchFeed, raw, 0600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed?market=US&symbol=AAPL", nil))
		var view c.PublicResearchView
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
			t.Fatalf("view: %d %s", w.Code, w.Body.String())
		}
		return view
	}
	view := request()
	if len(view.Items) != 3 || !strings.Contains(string(view.Items[1]), `"title":null`) || !strings.Contains(string(view.Items[1]), `"summary":null`) {
		t.Fatalf("nullable article changed: %s", view.Items)
	}
	feed["items"] = []any{}
	view = request()
	if view.Items == nil || len(view.Items) != 0 {
		t.Fatalf("empty items changed: %#v", view.Items)
	}
}

func TestPublicResearchWireFixtureBytes(t *testing.T) {
	svc := newResearchService()
	svc.researchFeed = filepath.Join(t.TempDir(), "feed.json")
	raw := researchFixture(t)
	if err := os.WriteFile(svc.researchFeed, raw, 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	svc.routes().ServeHTTP(w, httptest.NewRequest("GET", "/v1/research/feed?market=US&symbol=AAPL", nil))
	want := strings.Replace(strings.TrimSpace(string(raw)), `{"schema_version":"mimir-public-research.v1",`, `{"schema_version":"public-research-view.v1","feed_schema_version":"mimir-public-research.v1","observed_at":"2026-11-09T00:00:00.000000123Z","market":"US","symbol":"AAPL",`, 1) + "\n"
	if w.Code != 200 || w.Body.String() != want || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("fixture wire changed: %d %v\ngot %s\nwant %s", w.Code, w.Header(), w.Body.String(), want)
	}
}
