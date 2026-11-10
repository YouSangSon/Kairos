package sqlite

import (
	"context"
	"database/sql"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/migrations"
	"strconv"
	"strings"
	"testing"
	"time"
)

func downgradeBrokerReplay(t testing.TB, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TRIGGER events_replay_policy_guard; DROP TABLE ledger_replay_state; DROP TABLE ledger_replay_transition; DROP TRIGGER events_cash_void_guard; DELETE FROM schema_migrations WHERE version=22`); err != nil {
		t.Fatal(err)
	}
	script, err := migrations.Files.ReadFile("migrations/009_fx_exchange.sql")
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

// The module fixture installs its owner policy; maintenance migration and archive cases remain integration tests.
func installBrokerReplay(db *sql.DB) error {
	script, err := migrations.Files.ReadFile("migrations/022_ledger_replay_policy.sql")
	if err != nil {
		return err
	}
	if _, err := db.Exec(string(script)); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO schema_migrations VALUES(22,'2026-01-10T15:00:00Z')`)
	return err
}
func testCSV(rows ...string) []byte {
	return []byte("source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,corrects_source_event_id\n" + strings.Join(rows, "\n") + "\n")
}
func applyTestCSV(t *testing.T, svc *brokerTestService, key string, raw []byte) {
	t.Helper()
	preview, issue := svc.PreviewImport(context.Background(), raw)
	if issue != nil {
		t.Fatal(issue)
	}
	if _, issue := svc.ApplyImport(context.Background(), contracts.ApplyRequest{PreviewID: preview.PreviewID, IdempotencyKey: key}); issue != nil {
		t.Fatal(issue)
	}
}
func activateBrokerReplay(ctx context.Context, svc *brokerTestService, revision int64, events, snapshot string, now time.Time) (*contracts.ReplayPreview, error) {
	prior := svc.now
	svc.now = func() time.Time { return now }
	defer func() { svc.now = prior }()
	return svc.ActivateReplay(ctx, revision, events, snapshot)
}
func legacyChronologyService(t *testing.T) *brokerTestService {
	t.Helper()
	svc, _ := testBrokerService(t)
	applyTestCSV(t, svc, "legacy-chronology", testCSV(
		"b0,account-main,BUY,2026-01-01T00:00:00Z,AAPL,1,10,0,USD,-10",
		"b1,account-main,BUY,2026-01-01T00:00:00.1Z,AAPL,1,20,0,USD,-20",
		"s1,account-main,SELL,2026-01-01T00:00:00.2Z,AAPL,1,30,0,USD,30",
	))
	downgradeBrokerReplay(t, svc.db)
	return svc
}

func TestLedgerReplayTransitionIsExplicitAtomicAndReplayable(t *testing.T) {
	svc, _ := testBrokerService(t)
	body := testCSV(
		"b0,account-main,BUY,2026-01-01T00:00:00Z,AAPL,1,10,0,USD,-10",
		"b1,account-main,BUY,2026-01-01T00:00:00.1Z,AAPL,1,20,0,USD,-20",
		"s1,account-main,SELL,2026-01-01T00:00:00.2Z,AAPL,1,30,0,USD,30",
	)
	applyTestCSV(t, svc, "before-duplicate", body)
	svc.now = func() time.Time { return mustBrokerTime("2026-01-10T16:00:00Z") }
	applyTestCSV(t, svc, "duplicate-only", body)
	downgradeBrokerReplay(t, svc.db)
	ctx := context.Background()
	if err := installBrokerReplay(svc.db); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotFrom(ctx, newQueryer(svc.db)); err == nil {
		t.Fatal("unreviewed legacy replay served a current snapshot")
	}
	if err := proveLedgerReplayState(ctx, newQueryer(svc.db), false); err == nil {
		t.Fatal("legacy replay started normal server authority")
	}
	if _, err := svc.db.Exec(`INSERT OR REPLACE INTO ledger_replay_state VALUES(1,?,0)`, fifoCostBasisPolicy); err == nil {
		t.Fatal("SQL REPLACE bypassed the unreviewed legacy state")
	}
	if _, err := svc.db.Exec(`INSERT INTO events(event_id,source_event_id,account_id,type,occurred_at,currency,amount,receipt_id,recorded_at) VALUES('blocked','blocked','account-main','DEPOSIT','2026-01-02T00:00:00Z','USD','1','blocked','2026-01-10T15:01:00Z')`); err == nil {
		t.Fatal("legacy preparation allowed an unreviewed direct write")
	}
	preview, err := svc.PreviewReplay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.LegacySnapshot.RealizedPnL[0].Amount != "10" || preview.Snapshot.RealizedPnL[0].Amount != "20" || preview.LegacySnapshotSHA256 == preview.SnapshotSHA256 {
		t.Fatalf("preview hid the economic correction: %+v", preview)
	}
	if preview.LegacySnapshot.RecordedAt != "2026-01-10T16:00:00Z" || preview.Snapshot.RecordedAt != "2026-01-10T16:00:00Z" {
		t.Fatal("preview lost duplicate-only import metadata")
	}
	for _, expectation := range []struct {
		revision         int64
		events, snapshot string
	}{
		{preview.LedgerRevision + 1, preview.EventsSHA256, preview.SnapshotSHA256},
		{preview.LedgerRevision, strings.Repeat("0", 64), preview.SnapshotSHA256},
		{preview.LedgerRevision, preview.EventsSHA256, strings.Repeat("0", 64)},
	} {
		if _, err := activateBrokerReplay(ctx, svc, expectation.revision, expectation.events, expectation.snapshot, svc.now()); err == nil {
			t.Fatal("activation ignored changed preview expectations")
		}
		var count int
		if err := svc.db.QueryRow(`SELECT COUNT(*) FROM ledger_replay_transition`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed activation left a receipt: %d %v", count, err)
		}
	}
	if _, err := activateBrokerReplay(ctx, svc, preview.LedgerRevision, preview.EventsSHA256, preview.SnapshotSHA256, mustBrokerTime("2025-01-01T00:00:00Z")); err == nil {
		t.Fatal("backdated transition activated")
	}
	if _, err := svc.db.Exec(`UPDATE ledger_meta SET recorded_at='2026-01-10T16:00:30Z' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := activateBrokerReplay(ctx, svc, preview.LedgerRevision, preview.EventsSHA256, preview.SnapshotSHA256, mustBrokerTime("2026-01-10T16:01:00Z")); err == nil {
		t.Fatal("metadata-only change bypassed reviewed snapshot expectations")
	}
	preview, err = svc.PreviewReplay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activateBrokerReplay(ctx, svc, preview.LedgerRevision, preview.EventsSHA256, preview.SnapshotSHA256, mustBrokerTime("2026-01-10T16:01:00Z")); err != nil {
		t.Fatal(err)
	}
	current, err := snapshotFrom(ctx, newQueryer(svc.db))
	if err != nil {
		t.Fatal(err)
	}
	_, currentSHA, err := codec.JSONHash(current)
	if err != nil || currentSHA != preview.SnapshotSHA256 {
		t.Fatalf("activated snapshot differs from the reviewed result: %s %v", currentSHA, err)
	}
	if _, err := svc.db.Exec(`INSERT OR REPLACE INTO ledger_replay_transition SELECT * FROM ledger_replay_transition`); err == nil {
		t.Fatal("SQL REPLACE bypassed immutable transition evidence")
	}
	if _, err := svc.db.Exec(`INSERT OR REPLACE INTO ledger_replay_state VALUES(1,?,0)`, fifoCostBasisPolicy); err == nil {
		t.Fatal("SQL REPLACE erased the activated legacy origin")
	}
	svc.now = func() time.Time { return mustBrokerTime("2026-01-10T16:02:00Z") }
	applyTestCSV(t, svc, "after-upgrade", testCSV("s2,account-main,SELL,2026-01-02T00:00:00Z,AAPL,1,40,0,USD,40"))
	if err := proveLedgerReplayState(ctx, newQueryer(svc.db), false); err != nil {
		t.Fatal("later append changed historical transition proof:", err)
	}
}

func TestLedgerReplayRejectsInvalidHistoryAndTamperedTransition(t *testing.T) {
	for _, mutation := range []string{"missing_receipt", "changed_hash", "invalid_chronology"} {
		t.Run(mutation, func(t *testing.T) {
			svc := legacyChronologyService(t)
			if mutation == "invalid_chronology" {
				if _, err := svc.db.Exec(`DROP TRIGGER events_no_update; UPDATE events SET occurred_at=CASE WHEN source_event_id='b0' THEN '2026-01-01T00:00:00.2Z' ELSE '2026-01-01T00:00:00Z' END WHERE source_event_id IN ('b0','s1')`); err != nil {
					t.Fatal(err)
				}
			}
			if err := installBrokerReplay(svc.db); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			preview, err := svc.PreviewReplay(ctx)
			if mutation == "invalid_chronology" {
				if err == nil {
					t.Fatal("invalid chronological history was eligible for transition")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := activateBrokerReplay(ctx, svc, preview.LedgerRevision, preview.EventsSHA256, preview.SnapshotSHA256, svc.now()); err != nil {
				t.Fatal(err)
			}
			var sqlMutation string
			if mutation == "missing_receipt" {
				sqlMutation = `DROP TRIGGER ledger_replay_transition_no_delete; DELETE FROM ledger_replay_transition`
			} else {
				sqlMutation = `DROP TRIGGER ledger_replay_transition_no_update; UPDATE ledger_replay_transition SET snapshot_sha256='0000000000000000000000000000000000000000000000000000000000000000'`
			}
			if _, err := svc.db.Exec(sqlMutation); err != nil {
				t.Fatal(err)
			}
			guard := "ledger_replay_transition_no_update"
			if mutation == "missing_receipt" {
				guard = "ledger_replay_transition_no_delete"
			}
			script, err := migrations.Files.ReadFile("migrations/022_ledger_replay_policy.sql")
			if err != nil {
				t.Fatal(err)
			}
			prefix := "CREATE TRIGGER " + guard + " "
			_, after, found := strings.Cut(string(script), prefix)
			body, _, ended := strings.Cut(after, "END;")
			if !found || !ended {
				t.Fatal("missing original receipt guard")
			}
			if _, err := svc.db.Exec(prefix + body + "END;"); err != nil {
				t.Fatal(err)
			}
			if err := proveLedgerReplayState(ctx, newQueryer(svc.db), false); err == nil {
				t.Fatal("tampered replay transition passed current recovery")
			}
		})
	}
}

func TestLedgerReplayGenesisRejectsUncountedEvents(t *testing.T) {
	for _, legacyMigration := range []bool{false, true} {
		t.Run(strconv.FormatBool(legacyMigration), func(t *testing.T) {
			svc, _ := testBrokerService(t)
			if _, err := svc.db.Exec(`INSERT INTO events(event_id,source_event_id,account_id,type,occurred_at,currency,amount,receipt_id,recorded_at) VALUES('uncounted','uncounted','account-main','DEPOSIT','2026-01-01T00:00:00Z','USD','1','uncounted','2026-01-10T15:01:00Z')`); err != nil {
				t.Fatal(err)
			}
			if legacyMigration {
				downgradeBrokerReplay(t, svc.db)
				if err := installBrokerReplay(svc.db); err != nil {
					t.Fatal(err)
				}
			}
			if err := proveLedgerReplayState(context.Background(), newQueryer(svc.db), false); err == nil {
				t.Fatal("genesis recovery concealed existing events behind revision zero")
			}
			if _, err := snapshotFrom(context.Background(), newQueryer(svc.db)); err == nil {
				t.Fatal("current snapshot concealed existing events behind revision zero")
			}
		})
	}
}

func TestReplayActivationRollsBackTransitionWhenPolicyUpdateFails(t *testing.T) {
	svc := legacyChronologyService(t)
	if err := installBrokerReplay(svc.db); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewReplay(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`CREATE TRIGGER reject_test_policy_update BEFORE UPDATE ON ledger_replay_state BEGIN SELECT RAISE(ABORT,'injected policy update failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivateReplay(context.Background(), preview.LedgerRevision, preview.EventsSHA256, preview.SnapshotSHA256); err == nil {
		t.Fatal("failed policy update activated replay")
	}
	var count int
	var policy string
	if err := svc.db.QueryRow(`SELECT policy,(SELECT COUNT(*) FROM ledger_replay_transition) FROM ledger_replay_state WHERE singleton=1`).Scan(&policy, &count); err != nil {
		t.Fatal(err)
	}
	if policy != legacyFIFOCostBasisPolicy || count != 0 {
		t.Fatalf("partial replay activation survived: %s %d", policy, count)
	}
}
