package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"omni-folio/services/core/internal/modules/trading/contracts"
)

type readStub struct {
	calls int
	err   error
}

func (s *readStub) LocalOrderLog(context.Context) (*contracts.LocalOrderLog, error) {
	s.calls++
	return nil, s.err
}
func (s *readStub) PaperMonitor(context.Context) (*contracts.PaperMonitor, error) {
	s.calls++
	return nil, s.err
}
func (s *readStub) PaperPromotionReport(context.Context) (*contracts.PaperPromotionReportView, error) {
	s.calls++
	return nil, s.err
}
func (s *readStub) ResearchReport(context.Context) (*contracts.ResearchReportView, error) {
	s.calls++
	return nil, s.err
}

func TestTradingRoutesPreserveMethodMatchingAndErrorRedaction(t *testing.T) {
	for _, path := range []string{"/v1/orders", "/v1/paper/monitor", "/v1/paper/promotion-report", "/v1/research/report"} {
		t.Run(path, func(t *testing.T) {
			service := &readStub{err: errors.New("synthetic-private-provider-detail")}
			mux := http.NewServeMux()
			Register(mux, service)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Code != 500 || response.Body.String() != "{\"code\":\"internal_error\",\"message\":\"internal server error\"}\n" || strings.Contains(response.Body.String(), "private-provider") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("headers=%v", response.Header())
			}
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
			if response.Code != 405 || response.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("POST status=%d allow=%q", response.Code, response.Header().Get("Allow"))
			}
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("HEAD", path, nil))
			if response.Code != 500 {
				t.Fatalf("HEAD status=%d", response.Code)
			}
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("GET", path+"/unknown", nil))
			if response.Code != 404 {
				t.Fatalf("unknown status=%d", response.Code)
			}
		})
	}
}

func TestPaperAndResearchRoutesRejectQueryBeforeReading(t *testing.T) {
	for _, path := range []string{"/v1/paper/monitor", "/v1/paper/promotion-report", "/v1/research/report"} {
		service := &readStub{}
		mux := http.NewServeMux()
		Register(mux, service)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", path+"?x=1", nil))
		if response.Code != 400 || service.calls != 0 {
			t.Fatalf("path=%s status=%d calls=%d", path, response.Code, service.calls)
		}
	}
}
