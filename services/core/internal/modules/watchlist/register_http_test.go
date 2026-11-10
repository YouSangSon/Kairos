package watchlist_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	watchlist "omni-folio/services/core/internal/modules/watchlist"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"strings"
	"testing"
)

type api struct {
	read, replace int
	err           error
	change        c.WatchlistChange
	report        *c.WatchlistReport
}

func (a *api) Read(context.Context) (*c.WatchlistReport, error) { a.read++; return a.report, a.err }
func (a *api) Replace(_ context.Context, change c.WatchlistChange) (*c.WatchlistState, error) {
	a.replace++
	a.change = change
	return &c.WatchlistState{Revision: 1, Entries: change.Entries}, a.err
}
func request(mux *http.ServeMux, method, path, body, media string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", media)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

const valid = `{"expected_revision":0,"entries":[{"venue":"XNAS","symbol":"AAPL","currency":"USD"}]}`

func TestHTTPClosedJSONAndQueryContract(t *testing.T) {
	a := &api{}
	mux := http.NewServeMux()
	watchlist.RegisterHTTP(mux, a)
	bad := []string{`{}`, `null`, `{"expected_revision":0,"entries":null}`, valid + ` {}`, strings.Replace(valid, `"expected_revision":0`, `"expected_revision":0,"expected_revision":1`, 1), strings.Replace(valid, `"symbol":"AAPL"`, `"symbol":"AAPL","sy\u006dbol":"MSFT"`, 1), strings.Replace(valid, `"expected_revision":0`, `"expected_revision":0.0`, 1), strings.Replace(valid, `"expected_revision":0`, `"expected_revision":9007199254740992`, 1), strings.Replace(valid, `"AAPL"`, `"\ud800"`, 1), strings.Replace(valid, `"currency":"USD"`, `"currency":"USD","private":"secret"`, 1), valid + strings.Repeat(" ", c.BodyLimit)}
	for _, body := range bad {
		w := request(mux, "POST", "/v1/watchlist", body, "application/json")
		if w.Code != 400 || a.replace != 0 || !strings.Contains(w.Body.String(), "watchlist_invalid") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/v1/watchlist?", "/v1/watchlist?as_of=x"} {
		for _, method := range []string{"GET", "POST"} {
			if w := request(mux, method, path, valid, "application/json"); w.Code != 400 {
				t.Fatal(w.Code)
			}
		}
	}
	for _, media := range []string{"", "text/plain", "application/json; broken"} {
		if w := request(mux, "POST", "/v1/watchlist", valid, media); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w := request(mux, "POST", "/v1/watchlist", valid, "application/json; charset=utf-8")
	if w.Code != 200 || a.replace != 1 || a.change.ExpectedRevision != 0 || len(a.change.Entries) != 1 || a.change.Entries[0].Symbol != "AAPL" {
		t.Fatal(w.Code, a)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" || strings.HasSuffix(w.Body.String(), "\n") {
		t.Fatal(w.Header(), w.Body.String())
	}
}
func TestHTTPFailureMappingAndMuxSemantics(t *testing.T) {
	a := &api{report: &c.WatchlistReport{}}
	mux := http.NewServeMux()
	watchlist.RegisterHTTP(mux, a)
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{c.ErrInput, 400, "watchlist_invalid"}, {c.ErrChanged, 409, "watchlist_changed"}, {errors.New("private-sql-secret"), 503, "watchlist_unavailable"}} {
		a.err = tc.err
		for _, method := range []string{"GET", "POST"} {
			w := request(mux, method, "/v1/watchlist", valid, "application/json")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private-sql-secret") {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	a.err = nil
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/v1/watchlist", 200}, {"HEAD", "/v1/watchlist", 200}, {"DELETE", "/v1/watchlist", 405}, {"GET", "/v1/watchlist/", 404}, {"GET", "/v1/missing", 404}} {
		if w := request(mux, tc.method, tc.path, "", ""); w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
	a.report.ObservedAt = strings.Repeat("x", c.ResponseLimit)
	w := request(mux, "GET", "/v1/watchlist", "", "")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "watchlist_unavailable") {
		t.Fatal(w.Code)
	}
}
