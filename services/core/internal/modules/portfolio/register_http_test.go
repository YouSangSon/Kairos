package portfolio_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"omni-folio/services/core/internal/application/fault"
	"omni-folio/services/core/internal/ledgerdomain"
	portfolio "omni-folio/services/core/internal/modules/portfolio"
	c "omni-folio/services/core/internal/modules/portfolio/contracts"
	"strings"
	"testing"
	"time"
)

type queries struct {
	err        error
	called     string
	base, asOf string
}

func (q *queries) CashValuation(_ context.Context, base, at string) (*c.CashValuation, error) {
	q.called = "cash"
	q.base = base
	q.asOf = at
	return &c.CashValuation{}, q.err
}
func (q *queries) HoldingValuation(_ context.Context, at string) (*c.HoldingValuation, error) {
	q.called = "holding"
	q.asOf = at
	return &c.HoldingValuation{Lines: []c.HoldingValuationLine{{InstrumentID: "private-instrument", Symbol: "AAPL", Price: &c.HoldingValuationPrice{ObservationID: "private-observation", Source: "owner_declared", SourceVersion: "v1"}}, {Symbol: "MSFT", Price: &c.HoldingValuationPrice{Source: "local_fixture", SourceVersion: "private-version"}}}}, q.err
}
func (q *queries) CurrentValuation(context.Context) (*c.CurrentPortfolioValuation, error) {
	q.called = "current"
	return &c.CurrentPortfolioValuation{}, q.err
}
func (q *queries) XIRR(context.Context) (*c.PortfolioXIRR, error) {
	q.called = "xirr"
	return &c.PortfolioXIRR{}, q.err
}
func (q *queries) TWR(context.Context) (*c.PortfolioTWR, error) {
	q.called = "twr"
	return &c.PortfolioTWR{}, q.err
}
func (q *queries) TWRCheckpoints(context.Context) (*c.PortfolioTWRCheckpoints, error) {
	q.called = "twr/checkpoints"
	return &c.PortfolioTWRCheckpoints{PolicyVersion: q.asOf}, q.err
}
func (q *queries) Benchmarks(context.Context) (*c.PortfolioBenchmarks, error) {
	q.called = "benchmarks"
	return &c.PortfolioBenchmarks{}, q.err
}
func serve(mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}
func TestHTTPQueriesAndSingleMuxSemantics(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	q := &queries{}
	mux := http.NewServeMux()
	portfolio.RegisterHTTP(mux, q, func() time.Time { return now })
	cases := []struct {
		path, call string
		status     int
	}{
		{"cash-valuation?base_currency=USD&as_of=2026-01-01T00:00:00Z", "cash", 200},
		{"cash-valuation?base_currency=USD", "", 400}, {"cash-valuation?base_currency=USD&as_of=x&as_of=y", "", 400}, {"cash-valuation?unknown=x", "", 400}, {"cash-valuation?%zz", "", 400},
		{"holding-valuation", "holding", 200}, {"holding-valuation?as_of=explicit", "holding", 200}, {"holding-valuation?unknown=x", "", 400}, {"holding-valuation?as_of=x&as_of=y", "", 400},
		{"current-valuation", "current", 200}, {"current-valuation?", "current", 200}, {"current-valuation?x=y", "", 400},
		{"xirr", "xirr", 200}, {"xirr?", "xirr", 200}, {"xirr?x=y", "", 400}, {"twr", "twr", 200}, {"twr?", "twr", 200}, {"twr?x=y", "", 400},
		{"twr/checkpoints", "twr/checkpoints", 200}, {"twr/checkpoints?", "", 400}, {"benchmarks", "benchmarks", 200}, {"benchmarks?", "", 400},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			q.called = ""
			w := serve(mux, "GET", "/v1/portfolio/"+tc.path)
			if w.Code != tc.status || q.called != tc.call {
				t.Fatalf("%d %q %s", w.Code, q.called, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
				t.Fatal(w.Header())
			}
		})
	}
	serve(mux, "GET", "/v1/portfolio/holding-valuation")
	if q.asOf != now.Format(time.RFC3339Nano) {
		t.Fatal(q.asOf)
	}
	serve(mux, "GET", "/v1/portfolio/cash-valuation?base_currency=USD&as_of=at")
	if q.base != "USD" || q.asOf != "at" {
		t.Fatal(q)
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"HEAD", "/v1/portfolio/xirr", 200}, {"POST", "/v1/portfolio/xirr", 405}, {"GET", "/v1/portfolio/missing", 404}, {"GET", "/v1/portfolio/xirr/", 404}} {
		if w := serve(mux, tc.method, tc.path); w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}
func TestHTTPErrorMappingPrivacyAndResponseBound(t *testing.T) {
	q := &queries{}
	mux := http.NewServeMux()
	portfolio.RegisterHTTP(mux, q, time.Now)
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{&c.Failure{Code: "invalid_query", Message: "bad", Field: "as_of"}, 400, "invalid_query"}, {&c.Failure{Code: "stale", Message: "stale", Conflict: true}, 409, "stale"}, {&fault.Error{Status: 503, Body: ledgerdomain.APIError{Code: "unavailable", Message: "unavailable"}}, 503, "unavailable"}, {errors.New("private-sql-secret"), 500, "internal_error"}} {
		q.err = tc.err
		w := serve(mux, "GET", "/v1/portfolio/xirr")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private-sql-secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	q.err = nil
	w := serve(mux, "GET", "/v1/portfolio/holding-valuation")
	for _, private := range []string{"instrument_id", "observation_id", "private-version"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal(w.Body.String())
		}
	}
	if !strings.Contains(w.Body.String(), `"source_version":"v1"`) {
		t.Fatal(w.Body.String())
	}
	q.asOf = strings.Repeat("x", 3<<20)
	w = serve(mux, "GET", "/v1/portfolio/twr/checkpoints")
	if w.Code != 500 || strings.Contains(w.Body.String(), q.asOf) {
		t.Fatal(w.Code)
	}
}
