package composition

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pool "omni-folio/services/core/internal/storage/sqlite"
)

func migratedRuntime(t *testing.T) (*Runtime, string, Resources) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core.db")
	serial := 0
	resources := Resources{Now: func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }, ID: func(prefix string) string { serial++; return fmt.Sprintf("%s_%d", prefix, serial) }}
	session, err := resources.Maintenance().Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Migrations().Migrate(context.Background()); err != nil {
		session.Close()
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := resources.OpenServer(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	return runtime, path, resources
}
func TestRuntimeUsesOneMuxWithExactMethodAndPathBehavior(t *testing.T) {
	runtime, _, _ := migratedRuntime(t)
	routes := runtime.Routes()
	for _, scenario := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/healthz", 200}, {"HEAD", "/healthz", 200}, {"POST", "/healthz", 405},
		{"GET", "/readyz", 200}, {"GET", "/v1/status", 200}, {"POST", "/v1/status", 405},
		{"GET", "/v1/watchlist", 200}, {"GET", "/v1/portfolio/snapshot", 200},
		{"GET", "/v1/market-data/candles", 400}, {"POST", "/v1/market-data/candles", 405},
		{"GET", "/v1/market-data/candles/extra", 404}, {"GET", "/v1/unknown", 404},
	} {
		t.Run(scenario.method+scenario.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(scenario.method, scenario.path, nil))
			if response.Code != scenario.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
func TestRuntimeChecksStartupBeforeReadingFixture(t *testing.T) {
	_, path, resources := migratedRuntime(t)
	missing := filepath.Join(t.TempDir(), "missing.csv")
	if _, err := resources.OpenServer(path, missing, ""); err == nil || !strings.Contains(err.Error(), "open market fixture") {
		t.Fatalf("healthy DB did not reach fixture: %v", err)
	}
	db, err := pool.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=27`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := resources.OpenServer(path, missing, ""); err == nil || !strings.Contains(err.Error(), "unsupported schema history") {
		t.Fatalf("fixture took precedence over corrupt startup proof: %v", err)
	}
	// A failed server constructor must release its pool for the next command.
	db, err = pool.OpenReadWriteExisting(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(27,'2030-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeKeepsOwnerOriginRejectionOutsideModules(t *testing.T) {
	runtime, _, _ := migratedRuntime(t)
	routes := WithCORS(runtime.Routes(), "http://localhost:3000")
	for _, path := range []string{"/v1/watchlist", "/v1/market-data/imports/preview", "/v1/market-data/imports/apply"} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		request.Header.Set("Origin", "http://unapproved.invalid")
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, request)
		if response.Code != 403 || !strings.Contains(response.Body.String(), "origin_rejected") {
			t.Fatalf("path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
func TestStandaloneVerificationReadsManifestBeforeCandidate(t *testing.T) {
	resources := NewResources()
	candidate := filepath.Join(t.TempDir(), "must-not-be-created.db")
	err := resources.Maintenance().VerifyManifest(context.Background(), candidate, "missing-golden", "missing-manifest")
	if err == nil || !strings.Contains(err.Error(), "missing-manifest") {
		t.Fatalf("candidate was opened before manifest: %v", err)
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatalf("verification created candidate: %v", err)
	}
}
