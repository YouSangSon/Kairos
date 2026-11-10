package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	httpadapter "omni-folio/services/core/internal/modules/ledger/internal/adapters/http"
	"omni-folio/services/core/internal/storage/observation"
	storage "omni-folio/services/core/internal/storage/sqlite"
	"omni-folio/services/core/migrations"
	"path/filepath"
	"testing"
	"time"
)

type brokerTestService struct {
	*Service
	db  *sql.DB
	now func() time.Time
	id  func(string) string
}

func testBrokerService(t *testing.T) (*brokerTestService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "broker.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"001_init.sql", "003_broker_snapshots.sql", "005_ledger_events.sql", "008_cash_void.sql", "009_fx_exchange.sql", "022_ledger_replay_policy.sql"} {
		script, err := migrations.Files.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for version := 1; version <= 27; version++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, version, "2026-01-10T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	counters := map[string]int{}
	id := func(prefix string) string {
		counter := counters[prefix]
		counters[prefix]++
		return fmt.Sprintf("%s_test_%s", prefix, time.Unix(0, int64(counter)).Format("150405.000000000"))
	}
	return newBrokerService(db, func() time.Time { return mustBrokerTime("2026-01-10T15:01:00Z") }, id), path
}
func newBrokerService(db *sql.DB, now func() time.Time, id func(string) string) *brokerTestService {
	fixture := &brokerTestService{db: db, now: now, id: id}
	clock := func() time.Time { return fixture.now() }
	resolver, err := observation.New(db, clock).LedgerReader(ReadStatements())
	if err != nil {
		panic(err)
	}
	fixture.Service = NewService(db, clock, func(prefix string) string { return fixture.id(prefix) }, func() time.Duration { return 15 * time.Minute }, [32]byte{}, resolver)
	return fixture
}
func mustBrokerTime(raw string) time.Time {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		panic(err)
	}
	return value
}
func (s *brokerTestService) recordKiwoomSnapshot(ctx context.Context, account string, snapshot *contracts.BrokerSnapshot) (*contracts.BrokerKnownGoodSnapshot, error) {
	return s.RecordBrokerSnapshot(ctx, account, snapshot)
}
func (s *brokerTestService) latestKiwoomSnapshot(ctx context.Context, environment contracts.BrokerEnvironment, exchange contracts.BrokerExchange, account string) (*contracts.BrokerKnownGoodSnapshot, error) {
	return s.LatestBrokerSnapshot(ctx, environment, exchange, account)
}
func (s *brokerTestService) routes() *http.ServeMux {
	mux := http.NewServeMux()
	httpadapter.Register(mux, s.Service, [32]byte{})
	return mux
}
