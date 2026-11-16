package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"omni-folio/services/core/internal/modules/ledger/construction"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	cli "omni-folio/services/core/internal/modules/ledger/internal/adapters/cli"
	files "omni-folio/services/core/internal/modules/ledger/internal/adapters/files"
	httpadapter "omni-folio/services/core/internal/modules/ledger/internal/adapters/http"
	provider "omni-folio/services/core/internal/modules/ledger/internal/adapters/provider"
	app "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/internal/providers/kiwoom"
	pool "omni-folio/services/core/internal/storage/sqlite"
	"omni-folio/services/core/migrations"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func boundaryTestService(t *testing.T, times []time.Time, ids map[string][]string) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := boundaryOpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := boundaryMigrate(db); err != nil {
		t.Fatal(err)
	}
	clockIndex := 0
	clock := func() time.Time {
		if len(times) == 0 {
			return boundaryMustTime("2026-01-10T15:01:00Z").Add(time.Duration(clockIndex) * time.Millisecond)
		}
		if clockIndex >= len(times) {
			return times[len(times)-1].Add(time.Duration(clockIndex-len(times)+1) * time.Millisecond)
		}
		v := times[clockIndex]
		clockIndex++
		return v
	}
	counters := map[string]int{}
	id := func(prefix string) string {
		i := counters[prefix]
		counters[prefix]++
		if i < len(ids[prefix]) {
			return ids[prefix][i]
		}
		return prefix + "_test_" + time.Unix(0, int64(i)).Format("150405.000000000")
	}
	return conservationService(db, clock, id), path
}

func boundaryTestCSV(rows ...string) string {
	return "source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount\n" + strings.Join(rows, "\n") + "\n"
}

func boundaryMustTime(raw string) time.Time {
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		panic(err)
	}
	return v
}

func boundaryApplyTestCSV(t *testing.T, svc *Service, key, body string) {
	t.Helper()
	preview, appErr := svc.PreviewImport(context.Background(), []byte(body))
	if appErr != nil {
		t.Fatal(appErr)
	}
	if !preview.CanApply {
		t.Fatalf("test setup preview was rejected: %+v", preview)
	}
	if _, appErr := svc.ApplyImport(context.Background(), c.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: key}); appErr != nil {
		t.Fatal(appErr)
	}
}

func boundaryCashVoidCSV(rows ...string) string {
	return "source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,corrects_source_event_id\n" + strings.Join(rows, "\n") + "\n"
}

func boundarySeedKRXLedger(t *testing.T, svc *Service) {
	t.Helper()
	tx, err := svc.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, args := range [][]any{
		{"g4h-cash", "cash-krw", "DEPOSIT", "2026-01-01T00:00:00Z", nil, nil, nil, nil, "100000", "g4h-receipt"},
		{"g4h-buy", "buy-005930", "BUY", "2026-01-02T00:00:00Z", "005930", "7", "1000", "0", "-7000", "g4h-receipt"},
	} {
		if _, err := tx.Exec(`INSERT INTO events(event_id,source_event_id,account_id,type,occurred_at,instrument_id,symbol,quantity,price,fee,currency,amount,receipt_id,recorded_at)
			VALUES(?,?,'account-main',?,?,CASE WHEN ? IS NULL THEN NULL ELSE 'instrument_' || ? END,?,?,?,?, 'KRW',?,?,?)`,
			args[0], args[1], args[2], args[3], args[4], args[4], args[4], args[5], args[6], args[7], args[8], args[9], "2026-01-10T15:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`UPDATE ledger_meta SET revision=2,recorded_at='2026-01-10T15:00:00Z' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func boundaryG4hSnapshot(fetchedAt string) *c.BrokerSnapshot {
	return &c.BrokerSnapshot{
		Source: "kiwoom", Environment: KiwoomMock, Exchange: KiwoomKRX,
		AccountRef: "kiwoom_account_AAAAAAAAAAAAAAAAAAAAAAAA", MaskedAccount: "******3210",
		FetchedAt: fetchedAt, Complete: true, Currency: "KRW",
		Totals: c.BrokerTotals{
			PurchaseAmount: "10000", EvaluationAmount: "11000", UnrealizedPnL: "1000", ReturnRatePercent: "10",
			EstimatedAssets: "111000", LoanAmount: "0", CreditLoanAmount: "0", CreditLendingAmount: "0",
		},
		Positions: []c.BrokerPosition{
			{Symbol: "000660", Name: "SK하이닉스", Quantity: "2", TradableQuantity: "2", AveragePurchasePrice: "1000", CurrentPrice: "1100", PurchaseAmount: "2000", EvaluationAmount: "2200", UnrealizedPnL: "200", ReturnRatePercent: "10", WeightPercent: "20"},
			{Symbol: "005930", Name: "삼성전자", Quantity: "10", TradableQuantity: "10", AveragePurchasePrice: "800", CurrentPrice: "880", PurchaseAmount: "8000", EvaluationAmount: "8800", UnrealizedPnL: "800", ReturnRatePercent: "10", WeightPercent: "80"},
		},
		OpenOrders: []c.BrokerOpenOrder{},
	}
}

func boundaryLegacyChronologyService(t *testing.T) *Service {
	t.Helper()
	svc, _ := boundaryTestService(t, nil, nil)
	boundaryApplyTestCSV(t, svc, "legacy-chronology", boundaryTestCSV(
		"b0,account-main,BUY,2026-01-01T00:00:00Z,AAPL,1,10,0,USD,-10",
		"b1,account-main,BUY,2026-01-01T00:00:00.1Z,AAPL,1,20,0,USD,-20",
		"s1,account-main,SELL,2026-01-01T00:00:00.2Z,AAPL,1,30,0,USD,30",
	))
	boundaryDowngradeLedgerReplayForTest(t, svc.db)
	return svc
}

// Only ledger and the original side-effect tables are needed; root migration orchestration stays at the root.
func boundaryMigrate(db *sql.DB) error {
	var initialized int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='schema_migrations'`).Scan(&initialized); err != nil {
		return err
	}
	if initialized == 0 {
		for _, name := range []string{"001_init.sql", "002_orders.sql", "003_broker_snapshots.sql", "004_execution_authority.sql", "005_ledger_events.sql", "008_cash_void.sql", "009_fx_exchange.sql", "022_ledger_replay_policy.sql"} {
			script, err := migrations.Files.ReadFile(name)
			if err != nil {
				return err
			}
			if _, err = db.Exec(string(script)); err != nil {
				return err
			}
		}
		for version := 1; version <= 27; version++ {
			if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, version, "2026-01-10T15:00:00Z"); err != nil {
				return err
			}
		}
		return nil
	}
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=22`).Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		script, err := migrations.Files.ReadFile("022_ledger_replay_policy.sql")
		if err != nil {
			return err
		}
		if _, err = db.Exec(string(script)); err != nil {
			return err
		}
		_, err = db.Exec(`INSERT INTO schema_migrations VALUES(22,'2026-01-10T15:00:00Z')`)
		return err
	}
	return nil
}
func boundaryOpenDB(path string) (*sql.DB, error) { return pool.Open(path) }
func boundaryOpenReadOnlyExistingDB(ctx context.Context, path string) (*sql.DB, error) {
	return pool.OpenReadOnlyExisting(ctx, path)
}
func conservationService(db *sql.DB, now func() time.Time, id func(string) string) *Service {
	return NewService(db, now, id, func() time.Duration { return 15 * time.Minute }, [32]byte{}, nil)
}

type conservationSession struct{ api *Service }

func (s conservationSession) Ledger() c.Service { return s.api }
func (s conservationSession) RequireSchema() error {
	return requireSchemaContext(context.Background(), newQueryer(s.api.db))
}
func (s conservationSession) Close() error { return s.api.db.Close() }

type conservationResources struct{}

func (conservationResources) OpenExisting(path string) (construction.Session, error) {
	db, err := pool.OpenExisting(path)
	return conservationOpened(db, err)
}
func (conservationResources) OpenReadOnly(ctx context.Context, path string) (construction.Session, error) {
	db, err := pool.OpenReadOnlyExisting(ctx, path)
	return conservationOpened(db, err)
}
func (conservationResources) OpenReadWrite(ctx context.Context, path string) (construction.Session, error) {
	db, err := pool.OpenReadWriteExisting(ctx, path)
	return conservationOpened(db, err)
}
func conservationOpened(db *sql.DB, err error) (construction.Session, error) {
	if err != nil {
		return nil, err
	}
	return conservationSession{conservationService(db, mockSyncNow, func(p string) string { return p + "_conservation" })}, nil
}
func runKiwoomMockSync(ctx context.Context, args []string, out io.Writer, read func(context.Context) ([]byte, error), transport http.RoundTripper, now func() time.Time, pacer *kiwoom.MockReadPacer) error {
	factory := provider.MockSnapshots{ReadSecret: read, Transport: transport, Now: now, Pacer: pacer}
	return cli.Run(ctx, "kiwoom-mock-sync", args, conservationResources{}, out, io.Discard, func(b c.Broker) app.MockSync { return app.MockSync{Broker: b, Factory: factory} }, files.ExportPublisher{})
}
func runLedgerExportCommand(ctx context.Context, args []string, out io.Writer) error {
	return cli.Run(ctx, "ledger-export", args, conservationResources{}, out, io.Discard, nil, files.ExportPublisher{})
}
func publishLedgerExport(ctx context.Context, dbPath, path string, events []c.LedgerActivity) (string, error) {
	return (files.ExportPublisher{}).Publish(ctx, dbPath, path, events)
}
func mockSyncRoutes(svc *Service) *http.ServeMux {
	mux := http.NewServeMux()
	httpadapter.Register(mux, svc, [32]byte{})
	return mux
}
func newSyntheticMockSnapshot(t *testing.T) *kiwoom.MockSnapshot {
	t.Helper()
	client, err := kiwoom.NewMockSnapshot(kiwoom.MockSnapshotConfig{AppKey: "synthetic-app-key", SecretKey: "synthetic-secret-key", AccountNumber: "9876543210", AliasKey: []byte("0123456789abcdef0123456789abcdef"), Transport: kiwoomRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected request") }), Now: mockSyncNow, Pacer: mockSyncPacer()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}
func boundaryCopyFile(source, target string) (resultErr error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, in.Close()) }()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}
func boundaryDowngradeLedgerReplayForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TRIGGER events_replay_policy_guard; DROP TABLE ledger_replay_state; DROP TABLE ledger_replay_transition;
        DROP TRIGGER events_cash_void_guard; DELETE FROM schema_migrations WHERE version=22`); err != nil {
		t.Fatal(err)
	}
	script, err := migrations.Files.ReadFile("009_fx_exchange.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, body, found := strings.Cut(string(script), "CREATE TRIGGER events_cash_void_guard")
	if !found {
		t.Fatal("legacy cash-void guard fixture is missing")
	}
	if _, err := db.Exec("CREATE TRIGGER events_cash_void_guard" + body); err != nil {
		t.Fatal(err)
	}
}
