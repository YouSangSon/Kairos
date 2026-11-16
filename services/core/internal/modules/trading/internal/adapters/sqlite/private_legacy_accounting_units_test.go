package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	"reflect"
	"strings"
	"testing"
	"time"
)

type paperAccountingSideEffectCounts struct {
	Sessions, Orders, OrderEvents, RiskReservations, AuthorityEvents, LedgerEvents, FXObservations, SecurityPriceObservations int
}

func TestG38C1PaperAccountingSessionFailsClosed(t *testing.T) {
	t.Run("stale selection", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		second, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
			config["experiment_id"] = "paper-accounting-stale"
			config["strategy"].(map[string]any)["version"] = "1.0.4"
		}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.SelectPaperCandidate(ctx, second.ResultSHA256, selected.CurrentEventID)
		if err != nil {
			t.Fatal(err)
		}
		before := paperAccountingSideEffects(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err == nil {
			t.Fatal("stale selection opened an accounting session")
		}
		assertPaperAccountingSideEffectsUnchanged(t, before, paperAccountingSideEffects(t, svc))
	})

	t.Run("mismatched result", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		_, selected := selectedPaperStrategy(t, svc)
		other, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
			config["experiment_id"] = "paper-accounting-mismatch"
			config["strategy"].(map[string]any)["version"] = "1.0.5"
		}))
		if err != nil {
			t.Fatal(err)
		}
		before := paperAccountingSideEffects(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, other.ResultSHA256, selected.CurrentEventID); err == nil {
			t.Fatal("mismatched result opened an accounting session")
		}
		assertPaperAccountingSideEffectsUnchanged(t, before, paperAccountingSideEffects(t, svc))
	})

	t.Run("no strategy", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		if _, err := svc.RollbackPaperCandidate(ctx, selected.CurrentEventID, selected.CurrentEventID); err != nil {
			t.Fatal(err)
		}
		before := paperAccountingSideEffects(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err == nil {
			t.Fatal("no strategy opened an accounting session")
		}
		assertPaperAccountingSideEffectsUnchanged(t, before, paperAccountingSideEffects(t, svc))
	})

	t.Run("corrupt registry", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		if _, err := svc.db.Exec(`DROP TRIGGER strategy_research_evidence_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.db.Exec(`UPDATE strategy_research_evidence SET artifact_sha256=? WHERE result_sha256=?`, strings.Repeat("0", 64), evidence.ResultSHA256); err != nil {
			t.Fatal(err)
		}
		before := paperAccountingSideEffects(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err == nil {
			t.Fatal("corrupt registry opened an accounting session")
		}
		assertPaperAccountingSideEffectsUnchanged(t, before, paperAccountingSideEffects(t, svc))
	})

	t.Run("corrupt order", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		state := mustRecordK2AOrder(t, svc, "paper-accounting-corrupt-order")
		if _, err := svc.db.Exec(`DROP TRIGGER order_idempotency_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.db.Exec(`UPDATE order_idempotency SET request_sha256=? WHERE order_id=?`, strings.Repeat("0", 64), state.OrderID); err != nil {
			t.Fatal(err)
		}
		before := paperAccountingSideEffects(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err == nil {
			t.Fatal("corrupt order log opened an accounting session")
		}
		assertPaperAccountingSideEffectsUnchanged(t, before, paperAccountingSideEffects(t, svc))
	})

	t.Run("prior account paper order", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		signal := paperEvaluationSignal(evidence.ResultSHA256, selected.CurrentEventID, "paper-accounting-prior-order")
		downgradePaperAuthorizationForTest(t, svc.db.DB)
		if _, err := svc.RecordOrderIntent(ctx, paperOrderIntent(k2aAccountRef, signal, "1", "1000")); err != nil {
			t.Fatal(err)
		}
		if err := privateLegacyMigrate(svc); err != nil {
			t.Fatal(err)
		}
		before := paperAccountingSideEffects(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err == nil {
			t.Fatal("prior paper order opened an accounting session")
		}
		assertPaperAccountingSideEffectsUnchanged(t, before, paperAccountingSideEffects(t, svc))
	})
}

func TestG38C1PaperAccountingSessionDirectWriterAndReplayGuards(t *testing.T) {
	t.Run("direct stale and mismatched selection are rejected", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		stale := directPaperAccountingSession(t, svc, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
		second, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
			config["experiment_id"] = "paper-accounting-direct-stale"
			config["strategy"].(map[string]any)["version"] = "1.0.6"
		}))
		if err != nil {
			t.Fatal(err)
		}
		secondSelection, err := svc.SelectPaperCandidate(ctx, second.ResultSHA256, selected.CurrentEventID)
		if err != nil {
			t.Fatal(err)
		}
		if err := insertPaperAccountingSessionDirect(svc, stale); err == nil {
			t.Fatal("SQLite accepted a stale session selection")
		}
		mismatch := directPaperAccountingSession(t, svc, k2aAccountRef, second.ResultSHA256, secondSelection.CurrentEventID)
		mismatch.StrategyResultSHA256 = evidence.ResultSHA256
		mismatch.SessionID = paperAccountingSessionID(mismatch.AccountRef, mismatch.StrategyResultSHA256, mismatch.StrategySelectionEventID, mismatch.ExecutionPolicySHA256)
		if err := insertPaperAccountingSessionDirect(svc, mismatch); err == nil {
			t.Fatal("SQLite accepted a mismatched session result")
		}
	})

	t.Run("direct prior paper order is rejected", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		signal := paperEvaluationSignal(evidence.ResultSHA256, selected.CurrentEventID, "paper-accounting-direct-prior")
		downgradePaperAuthorizationForTest(t, svc.db.DB)
		if _, err := svc.RecordOrderIntent(ctx, paperOrderIntent(k2aAccountRef, signal, "1", "1000")); err != nil {
			t.Fatal(err)
		}
		if err := privateLegacyMigrate(svc); err != nil {
			t.Fatal(err)
		}
		if err := insertPaperAccountingSessionDirect(svc, directPaperAccountingSession(t, svc, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)); err == nil {
			t.Fatal("SQLite accepted a session after a paper order")
		}
	})

	t.Run("update and delete are rejected", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		session, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`UPDATE paper_accounting_sessions SET currency='USD'`,
			`DELETE FROM paper_accounting_sessions`,
		} {
			if _, err := svc.db.Exec(statement); err == nil {
				t.Fatalf("SQLite accepted session mutation: %s", statement)
			}
		}
		if proof, err := provePaperAccountingRecovery(ctx, svc.db); err != nil || proof.Sessions != 1 || proof.SHA256 == "" || session.SessionID == "" {
			t.Fatalf("session proof=%+v err=%v", proof, err)
		}
	})

	t.Run("order recovery corruption", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		ctx := context.Background()
		evidence, selected := selectedPaperStrategy(t, svc)
		if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
			t.Fatal(err)
		}
		state := mustRecordK2AOrder(t, svc, "paper-accounting-proof-corrupt-order")
		if _, err := svc.db.Exec(`DROP TRIGGER order_idempotency_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.db.Exec(`UPDATE order_idempotency SET request_sha256=? WHERE order_id=?`, strings.Repeat("0", 64), state.OrderID); err != nil {
			t.Fatal(err)
		}
		if _, err := provePaperAccountingRecovery(ctx, svc.db); err == nil {
			t.Fatal("accounting recovery certified a corrupt order log")
		}
		if count := paperAccountingSessionCount(t, svc); count != 1 {
			t.Fatalf("order-proof corruption changed session count=%d", count)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(*Store, *PaperAccountingSession)
	}{
		{
			name: "policy corruption",
			mutate: func(svc *Store, session *PaperAccountingSession) {
				if _, err := svc.db.Exec(`DROP TRIGGER paper_accounting_sessions_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.db.Exec(`UPDATE paper_accounting_sessions SET execution_policy_json='{}' WHERE session_id=?`, session.SessionID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "record corruption",
			mutate: func(svc *Store, session *PaperAccountingSession) {
				if _, err := svc.db.Exec(`DROP TRIGGER paper_accounting_sessions_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.db.Exec(`UPDATE paper_accounting_sessions SET record_sha256=? WHERE session_id=?`, strings.Repeat("0", 64), session.SessionID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "identity corruption",
			mutate: func(svc *Store, session *PaperAccountingSession) {
				if _, err := svc.db.Exec(`DROP TRIGGER paper_accounting_sessions_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.db.Exec(`UPDATE paper_accounting_sessions SET session_id='paper_accounting_session_corrupt' WHERE session_id=?`, session.SessionID); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _ := testService(t, nil, nil)
			ctx := context.Background()
			evidence, selected := selectedPaperStrategy(t, svc)
			session, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(svc, session)
			if _, err := provePaperAccountingRecovery(ctx, svc.db); err == nil {
				t.Fatal("corrupt accounting session was certified")
			}
		})
	}
}

func paperAccountingSideEffects(t testing.TB, svc *Store) paperAccountingSideEffectCounts {
	t.Helper()
	count := func(table string) int {
		var value int
		if err := svc.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&value); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return value
	}
	return paperAccountingSideEffectCounts{
		Sessions: count("paper_accounting_sessions"), Orders: count("order_idempotency"), OrderEvents: count("order_events"),
		RiskReservations: count("risk_reservations"), AuthorityEvents: count("execution_authority_events"), LedgerEvents: count("events"),
		FXObservations: count("fx_observations"), SecurityPriceObservations: count("security_price_observations"),
	}
}

func assertPaperAccountingSideEffectsUnchanged(t testing.TB, before, after paperAccountingSideEffectCounts) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("session failure changed durable side effects: before=%+v after=%+v", before, after)
	}
}

type g38c3TwoFill struct {
	svc                                   *Store
	firstBar, secondBar                   *PaperMarketBarObservation
	firstFillSequence, secondFillSequence int64
}

func TestG38C3PaperPerformanceRejectsLaterCorruptionAndLegacy(t *testing.T) {
	t.Run("older retry accepts valid later evidence", func(t *testing.T) {
		svc, asOf := g38c3CashPerformanceFixture(t)
		first, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf)
		if err != nil {
			t.Fatal(err)
		}
		svc.now = func() time.Time { return mustTime("2026-01-12T07:00:00Z") }
		recordG38C3MarkBar(t, svc, "005930", "g38c3-later-valid", "2026-01-12T06:30:00.000000000Z", "100")
		retry, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf)
		if err != nil || retry.PerformanceID != first.PerformanceID {
			t.Fatalf("older retry=%+v err=%v", retry, err)
		}
	})

	t.Run("older retry proves later order evidence", func(t *testing.T) {
		svc, asOf := g38c3CashPerformanceFixture(t)
		if _, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf); err != nil {
			t.Fatal(err)
		}
		state := mustRecordK2AOrder(t, svc, "g38c3-later-corrupt-order")
		if _, err := svc.db.Exec(`DROP TRIGGER order_events_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.db.Exec(`UPDATE order_events SET event_sha256=? WHERE order_id=?`, strings.Repeat("0", 64), state.OrderID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf); err == nil {
			t.Fatal("older retry hid later corrupt order evidence")
		}
	})

	t.Run("older retry proves later market evidence", func(t *testing.T) {
		svc, asOf := g38c3CashPerformanceFixture(t)
		if _, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf); err != nil {
			t.Fatal(err)
		}
		svc.now = func() time.Time { return mustTime("2026-01-12T07:00:00Z") }
		later := recordG38C3MarkBar(t, svc, "005930", "g38c3-later-corrupt", "2026-01-12T06:30:00.000000000Z", "100")
		if _, err := svc.db.Exec(`DROP TRIGGER paper_market_bar_observations_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.db.Exec(`UPDATE paper_market_bar_observations SET record_sha256=? WHERE observation_id=?`, strings.Repeat("0", 64), later.ObservationID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf); err == nil {
			t.Fatal("older retry hid later corrupt market evidence")
		}
	})

	t.Run("account-scoped legacy v1 and v2 orders fail closed", func(t *testing.T) {
		for _, schema := range []string{paperSignalSchema, "paper-signal.v2"} {
			svc, _ := testService(t, nil, nil)
			svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
			evidence, selected := selectedPaperStrategy(t, svc)
			if _, err := svc.OpenPaperAccountingSession(context.Background(), k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
				t.Fatal(err)
			}
			downgradePaperAuthorizationForTest(t, svc.db.DB)
			legacy := paperEvaluationSignal(evidence.ResultSHA256, selected.CurrentEventID, "g38c3-legacy-"+schema)
			legacy.SchemaVersion = schema
			intent := paperOrderIntent(k2aAccountRef, legacy, "1", "1000")
			intent.ClientOrderID = "g38c3-legacy-" + schema
			if err := insertG38C2LegacyPaperOrderDirect(svc, intent, "order_g38c3_legacy"); err != nil {
				t.Fatal(err)
			}
			if err := privateLegacyMigrate(svc); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.EvaluatePaperPerformance(context.Background(), k2aAccountRef, "2026-01-11T06:30:00.000000000Z"); err == nil {
				t.Fatalf("legacy %s account was evaluated", schema)
			}
		}
	})
}

func TestG38C3BoundedAccounting(t *testing.T) {
	t.Run("excludes fills after the order sequence cutoff", func(t *testing.T) {
		fixture := g38c3TwoFillFixture(t)
		states, err := replayPaperAccountingAt(context.Background(), fixture.svc.db, paperAccountingCutoff{
			OrderSequence: fixture.firstFillSequence,
			AsOf:          fixture.secondBar.CloseAt,
		})
		if err != nil || states[k2aAccountRef].CapitalizedFills != 1 {
			t.Fatalf("bounded state=%+v err=%v", states[k2aAccountRef], err)
		}
	})

	t.Run("excludes a fill whose bar has not closed", func(t *testing.T) {
		fixture := g38c3TwoFillFixture(t)
		states, err := replayPaperAccountingAt(context.Background(), fixture.svc.db, paperAccountingCutoff{
			OrderSequence: fixture.secondFillSequence,
			AsOf:          "2026-01-11T03:00:00.000000000Z",
		})
		if err != nil || states[k2aAccountRef].CapitalizedFills != 1 {
			t.Fatalf("bounded state=%+v err=%v", states[k2aAccountRef], err)
		}
	})

	t.Run("validates a corrupt fill after the cutoff", func(t *testing.T) {
		fixture := g38c3TwoFillFixture(t)
		var raw string
		if err := fixture.svc.db.QueryRow(`SELECT event_json FROM order_events WHERE sequence=?`, fixture.secondFillSequence).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var event OrderEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		event.Price = "101"
		canonical, hash, err := orderJSONHash(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.svc.db.Exec(`DROP TRIGGER order_events_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.svc.db.Exec(`UPDATE order_events SET event_json=?,event_sha256=? WHERE sequence=?`, string(canonical), hash, fixture.secondFillSequence); err != nil {
			t.Fatal(err)
		}
		if _, err := replayPaperAccountingAt(context.Background(), fixture.svc.db, paperAccountingCutoff{
			OrderSequence: fixture.firstFillSequence,
			AsOf:          fixture.firstBar.CloseAt,
		}); err == nil {
			t.Fatal("bounded replay hid a corrupt fill after its cutoff")
		}
	})

	t.Run("rejects a legacy paper account instead of returning cash only", func(t *testing.T) {
		svc, _ := testService(t, nil, nil)
		svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
		evidence, selected := selectedPaperStrategy(t, svc)
		if _, err := svc.OpenPaperAccountingSession(context.Background(), k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
			t.Fatal(err)
		}
		downgradePaperAuthorizationForTest(t, svc.db.DB)
		legacy := PaperSignal{
			SchemaVersion: paperSignalSchema, SignalID: "g38c3-legacy-performance",
			StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID,
			DataSHA256: strings.Repeat("d", 64), Symbol: "005930", TargetQuantity: "2",
			DataAsOf: "2026-01-10T14:59:00Z", GeneratedAt: "2026-01-10T14:59:01Z", ExpiresAt: "2026-01-10T15:01:00Z",
		}
		lease := mustK2CLease(t, svc, k2aAccountRef)
		if state, err := svc.RunPaperSignal(context.Background(), k2aAccountRef, legacy, PaperMarketObservation{
			Source: "local_fixture", Symbol: legacy.Symbol, ObservedAt: "2026-01-10T15:00:00Z", AskPrice: "999", AvailableQuantity: "2",
		}, lease.FencingToken); err != nil || state.Status != "FILLED" {
			t.Fatalf("legacy state=%+v err=%v", state, err)
		}
		if err := privateLegacyMigrate(svc); err != nil {
			t.Fatal(err)
		}
		var cutoff int64
		if err := svc.db.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM order_events`).Scan(&cutoff); err != nil {
			t.Fatal(err)
		}
		if _, err := replayPaperAccountingAt(context.Background(), svc.db, paperAccountingCutoff{
			OrderSequence: cutoff,
			AsOf:          "2026-01-10T15:00:00.000000000Z",
		}); err == nil {
			t.Fatal("bounded replay accepted a legacy paper account")
		}
	})
}

func g38c3TwoFillFixture(t *testing.T) g38c3TwoFill {
	t.Helper()
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	svc.now = func() time.Time { return mustTime("2026-01-15T07:00:00Z") }
	signal.TargetQuantity, signal.ExpiresAt = "4", "2026-01-20T00:00:00.000000000Z"
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	first := recordG38C2FillBar(t, svc, "g38c3-first-fill", "100", "5", "2026-01-10")
	if state, err := svc.RunPaperOrder(context.Background(), order.OrderID, lease.FencingToken); err != nil || state.FilledQuantity != "2" {
		t.Fatalf("first fill state=%+v err=%v", state, err)
	}
	second := recordG38C2FillBar(t, svc, "g38c3-second-fill", "100", "5", "2026-01-11")
	if state, err := svc.RunPaperOrder(context.Background(), order.OrderID, lease.FencingToken); err != nil || state.FilledQuantity != "4" {
		t.Fatalf("second fill state=%+v err=%v", state, err)
	}
	rows, err := svc.db.Query(`SELECT sequence FROM order_events WHERE order_id=? AND event_type='FILL_RECORDED' ORDER BY sequence`, order.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var sequences []int64
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		sequences = append(sequences, sequence)
	}
	if err := rows.Err(); err != nil || len(sequences) != 2 {
		t.Fatalf("fill sequences=%v err=%v", sequences, err)
	}
	return g38c3TwoFill{svc: svc, firstBar: first, secondBar: second, firstFillSequence: sequences[0], secondFillSequence: sequences[1]}
}

func TestG38C2PaperSignalCutoffFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*testing.T) (*Store, string, PaperSignal)
		mutate func(*testing.T, *Store, *string, *PaperSignal)
	}{
		{
			name: "missing session",
			setup: func(t *testing.T) (*Store, string, PaperSignal) {
				svc, signal, _ := g38c2PaperSignalFixture(t, false)
				return svc, k2aAccountRef, signal
			},
		},
		{
			name: "policy mismatch",
			setup: func(t *testing.T) (*Store, string, PaperSignal) {
				svc, signal, _ := g38c2PaperSignalFixture(t, true)
				ctx := context.Background()
				next, err := svc.registerStrategyEvidence(ctx, rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) {
					result["experiment_id"] = "g38c2-policy-mismatch"
					result["execution"].(map[string]any)["starting_cash"] = "20000"
				}))
				if err != nil {
					t.Fatal(err)
				}
				selected, err := svc.SelectPaperCandidate(ctx, next.ResultSHA256, signal.StrategySelectionEventID)
				if err != nil {
					t.Fatal(err)
				}
				signal.StrategyResultSHA256, signal.StrategySelectionEventID = next.ResultSHA256, selected.CurrentEventID
				return svc, k2aAccountRef, signal
			},
		},
		{
			name: "stale selection",
			setup: func(t *testing.T) (*Store, string, PaperSignal) {
				svc, signal, _ := g38c2PaperSignalFixture(t, true)
				ctx := context.Background()
				next, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
					config["experiment_id"] = "g38c2-stale-selection"
					config["strategy"].(map[string]any)["version"] = "1.0.9"
				}))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := svc.SelectPaperCandidate(ctx, next.ResultSHA256, signal.StrategySelectionEventID); err != nil {
					t.Fatal(err)
				}
				return svc, k2aAccountRef, signal
			},
		},
		{name: "data hash mismatch", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) {
			signal.DataSHA256 = strings.Repeat("b", 64)
		}},
		{name: "data as of mismatch", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) {
			signal.DataAsOf = "2026-01-09T06:29:59Z"
		}},
		{name: "generation before availability", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) {
			signal.GeneratedAt = "2026-01-09T06:30:59Z"
		}},
		{name: "future generation", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) {
			signal.GeneratedAt, signal.ExpiresAt = "2026-01-10T07:00:00.000000001Z", "2026-01-11T00:00:00Z"
		}},
		{name: "expired signal", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) {
			signal.ExpiresAt = "2026-01-10T06:59:59Z"
		}},
		{name: "noncanonical zero", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) { signal.TargetQuantity = "00" }},
		{name: "negative target", mutate: func(_ *testing.T, _ *Store, _ *string, signal *PaperSignal) { signal.TargetQuantity = "-1" }},
		{
			name: "legacy paper order",
			mutate: func(t *testing.T, svc *Store, _ *string, signal *PaperSignal) {
				downgradePaperAuthorizationForTest(t, svc.db.DB)
				insertG38C2LegacyPaperOrder(t, svc, *signal)
				if err := privateLegacyMigrate(svc); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var svc *Store
			var account string
			var signal PaperSignal
			if test.setup != nil {
				svc, account, signal = test.setup(t)
			} else {
				svc, signal, _ = g38c2PaperSignalFixture(t, true)
				account = k2aAccountRef
			}
			if test.mutate != nil {
				test.mutate(t, svc, &account, &signal)
			}
			before := g38c2PaperSignalSideEffects(t, svc)
			if _, err := recordG38C2PaperSignalForTest(svc, account, signal); err == nil {
				t.Fatal("invalid signal cutoff was accepted")
			}
			assertG38C2PaperSignalSideEffects(t, before, g38c2PaperSignalSideEffects(t, svc))
		})
	}
}

func insertG38C2LegacyPaperOrder(t testing.TB, svc *Store, signal PaperSignal) {
	t.Helper()
	intent := g38c2LegacyPaperIntent(signal, "g38c2-legacy-before-v3")
	if err := insertG38C2LegacyPaperOrderDirect(svc, intent, "order_g38c2_legacy"); err != nil {
		t.Fatal(err)
	}
}

func TestG38C2PaperAccountingIgnoresLegacyFills(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T15:00:00Z") }
	evidence, err := svc.registerStrategyEvidence(context.Background(), strategyArtifact(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(context.Background(), evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	downgradePaperAuthorizationForTest(t, svc.db.DB)
	signal := PaperSignal{
		SchemaVersion: paperSignalSchema, SignalID: "g38c2-legacy-accounting-fill",
		StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID,
		DataSHA256: strings.Repeat("d", 64), Symbol: "005930", TargetQuantity: "2",
		DataAsOf: "2026-01-10T14:59:00Z", GeneratedAt: "2026-01-10T14:59:01Z", ExpiresAt: "2026-01-10T15:01:00Z",
	}
	observation := PaperMarketObservation{
		Source: "local_fixture", Symbol: signal.Symbol, ObservedAt: "2026-01-10T15:00:00Z",
		AskPrice: "999", AvailableQuantity: "2",
	}
	lease := mustK2CLease(t, svc, k2aAccountRef)
	state, err := svc.RunPaperSignal(context.Background(), k2aAccountRef, signal, observation, lease.FencingToken)
	if err != nil || state.Status != "FILLED" {
		t.Fatalf("legacy fill state=%+v err=%v", state, err)
	}
	if err := privateLegacyMigrate(svc); err != nil {
		t.Fatal(err)
	}
	states, err := replayPaperAccounting(context.Background(), svc.db)
	proof, proofErr := provePaperAccountingRecovery(context.Background(), svc.db)
	if err != nil || proofErr != nil || len(states) != 0 || proof.CapitalizedFills != 0 {
		t.Fatalf("legacy fill changed capitalized accounting: states=%+v proof=%+v errors=(%v,%v)", states, proof, err, proofErr)
	}
}

func downgradePaperAuthorizationForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	downgradePaperPerformanceForTest(t, db)
	if _, err := db.Exec(`DROP TRIGGER paper_execution_authorizations_no_update;
		DROP TRIGGER paper_execution_authorizations_no_delete;
		DROP TRIGGER paper_execution_authorizations_state_guard;
		DROP TRIGGER paper_signal_events_capitalized_quantity_guard;
		DROP TRIGGER order_idempotency_capitalized_paper_guard;
		DROP TRIGGER order_idempotency_legacy_paper_signal_guard;
		DROP TRIGGER order_events_risk_reservation_guard;
		DROP TRIGGER order_events_dispatch_reservation_guard;
		DROP TRIGGER order_events_non_authority_reservation_guard;
		DROP TRIGGER order_events_capitalized_paper_fill_guard;
		DROP TABLE paper_execution_authorizations;
		ALTER TABLE order_events DROP COLUMN paper_authorization_id;
		CREATE TRIGGER order_idempotency_legacy_paper_signal_guard BEFORE INSERT ON order_idempotency
		WHEN NEW.mode='paper' AND json_extract(NEW.intent_json, '$.signal_schema_version') IN ('paper-signal.v1','paper-signal.v2')
		AND EXISTS (SELECT 1 FROM paper_signal_events WHERE account_ref=NEW.account_ref)
		BEGIN SELECT RAISE(ABORT, 'legacy paper order cannot follow a v3 signal'); END;
		CREATE TRIGGER order_events_risk_reservation_guard BEFORE INSERT ON order_events WHEN NEW.event_type='RISK_APPROVED' BEGIN
			SELECT CASE WHEN NEW.authority_reservation_id IS NULL OR NOT EXISTS (SELECT 1 FROM risk_reservations WHERE reservation_id=NEW.authority_reservation_id AND order_id=NEW.order_id AND risk_event_id=NEW.event_id AND reservation_id=json_extract(NEW.event_json, '$.risk_reservation_id') AND policy_version=json_extract(NEW.event_json, '$.risk_policy_version') AND fencing_token=json_extract(NEW.event_json, '$.fencing_token')) THEN RAISE(ABORT, 'risk approval requires an authority reservation') END;
		END;
		CREATE TRIGGER order_events_dispatch_reservation_guard BEFORE INSERT ON order_events WHEN NEW.event_type='SUBMIT_DISPATCHED' BEGIN
			SELECT CASE WHEN NEW.authority_reservation_id IS NULL OR NOT EXISTS (SELECT 1 FROM risk_reservations WHERE reservation_id=NEW.authority_reservation_id AND order_id=NEW.order_id AND dispatch_event_id=NEW.event_id AND reservation_id=json_extract(NEW.event_json, '$.risk_reservation_id') AND policy_version=json_extract(NEW.event_json, '$.risk_policy_version') AND fencing_token=json_extract(NEW.event_json, '$.fencing_token')) THEN RAISE(ABORT, 'submit dispatch requires an authority reservation') END;
		END;
		CREATE TRIGGER order_events_non_authority_reservation_guard BEFORE INSERT ON order_events WHEN NEW.event_type NOT IN ('RISK_APPROVED','SUBMIT_DISPATCHED') AND NEW.authority_reservation_id IS NOT NULL BEGIN
			SELECT RAISE(ABORT, 'authority reservation is invalid for this event');
		END;
		DELETE FROM schema_migrations WHERE version=17`); err != nil {
		t.Fatal(err)
	}
}

func downgradePaperPerformanceForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	downgradePaperStrategyPerformanceForTest(t, db)
	if _, err := db.Exec(`DROP TRIGGER paper_performance_events_state_guard;
		DROP TRIGGER paper_performance_events_no_update;
		DROP TRIGGER paper_performance_events_no_delete;
		DROP INDEX paper_performance_events_account_idx;
		DROP TABLE paper_performance_events;
		DELETE FROM schema_migrations WHERE version=18`); err != nil {
		t.Fatal(err)
	}
}

func downgradePaperStrategyPerformanceForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	var currentVersion int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion); err != nil {
		t.Fatal(err)
	}
	if currentVersion >= 20 {
		downgradePaperPerformancePolicyForTest(t, db)
	}
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS paper_strategy_performance_events_state_guard;
		DROP TRIGGER IF EXISTS paper_strategy_performance_events_no_update;
		DROP TRIGGER IF EXISTS paper_strategy_performance_events_no_delete;
		DROP INDEX IF EXISTS paper_strategy_performance_events_window_idx;
		DROP TABLE IF EXISTS paper_strategy_performance_events;
		DELETE FROM schema_migrations WHERE version=19`); err != nil {
		t.Fatal(err)
	}
}

func downgradePaperPerformancePolicyForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	downgradeLedgerReplayForTest(t, db)
	var currentVersion int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion); err != nil {
		t.Fatal(err)
	}
	if currentVersion >= 21 {
		if _, err := db.Exec(`DROP TABLE paper_runner_leases;
			DROP INDEX strategy_selection_events_runner_binding_idx;
			DELETE FROM schema_migrations WHERE version=21`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`
		DROP TRIGGER paper_performance_policy_events_no_update;
		DROP TRIGGER paper_performance_policy_events_no_delete;
		DROP TRIGGER paper_performance_policy_events_state_guard;
		DROP TRIGGER execution_authority_events_state_guard;
		DROP TRIGGER strategy_selection_events_state_guard;
		DROP TRIGGER execution_authority_events_no_update;
		DROP TRIGGER execution_authority_events_no_delete;
		DROP TRIGGER strategy_selection_events_no_update;
		DROP TRIGGER strategy_selection_events_no_delete;
		DROP INDEX paper_performance_policy_events_account_selection_idx;
		DROP INDEX execution_authority_latest_idx;
		CREATE TABLE execution_authority_events_v19 (
			sequence INTEGER PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,account_ref TEXT NOT NULL,armed INTEGER NOT NULL CHECK (armed IN (0,1)),
			lease_owner TEXT,fencing_token INTEGER NOT NULL CHECK (fencing_token>0),lease_expires_at TEXT,
			reason_code TEXT NOT NULL CHECK (reason_code IN ('manual_arm','manual_halt','lease_acquired')),
			record_sha256 TEXT NOT NULL,record_json TEXT NOT NULL,recorded_at TEXT NOT NULL,
			UNIQUE(account_ref,fencing_token),CHECK((lease_owner IS NULL AND lease_expires_at IS NULL) OR (armed=1 AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)),CHECK(armed=1 OR lease_owner IS NULL)
		) STRICT;
		INSERT INTO execution_authority_events_v19(sequence,event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,record_sha256,record_json,recorded_at)
		SELECT sequence,event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,record_sha256,record_json,recorded_at FROM execution_authority_events;
		CREATE TABLE strategy_selection_events_v19 (
			sequence INTEGER PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,event_type TEXT NOT NULL CHECK(event_type IN ('SELECT','ROLLBACK')),
			candidate_result_sha256 TEXT REFERENCES strategy_research_evidence(result_sha256),expected_current_event_id TEXT NOT NULL,
			source_event_id TEXT REFERENCES strategy_selection_events(event_id),previous_selected_result_sha256 TEXT NOT NULL,selected_result_sha256 TEXT NOT NULL,
			reason_code TEXT NOT NULL CHECK(reason_code IN ('manual_selection','manual_rollback')),
			paper_evaluation_sequence INTEGER NOT NULL DEFAULT 0 CHECK(paper_evaluation_sequence>=0),record_sha256 TEXT NOT NULL CHECK(length(record_sha256)=64 AND record_sha256 NOT GLOB '*[^0-9a-f]*'),record_json TEXT NOT NULL,recorded_at TEXT NOT NULL,
			CHECK((event_type='SELECT' AND candidate_result_sha256 IS NOT NULL AND source_event_id IS NULL AND reason_code='manual_selection') OR (event_type='ROLLBACK' AND candidate_result_sha256 IS NULL AND source_event_id IS NOT NULL AND reason_code='manual_rollback'))
		) STRICT;
		INSERT INTO strategy_selection_events_v19(sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,record_sha256,record_json,recorded_at)
		SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,record_sha256,record_json,recorded_at FROM strategy_selection_events;
		DROP TABLE paper_performance_policy_events;
		PRAGMA legacy_alter_table=ON;
		ALTER TABLE execution_authority_events RENAME TO execution_authority_events_v20_old;
		ALTER TABLE strategy_selection_events RENAME TO strategy_selection_events_v20_old;
		ALTER TABLE execution_authority_events_v19 RENAME TO execution_authority_events;
		ALTER TABLE strategy_selection_events_v19 RENAME TO strategy_selection_events;
		DROP TABLE execution_authority_events_v20_old;
		DROP TABLE strategy_selection_events_v20_old;
		PRAGMA legacy_alter_table=OFF;
		CREATE INDEX execution_authority_latest_idx ON execution_authority_events(account_ref,sequence DESC);
		CREATE TRIGGER execution_authority_events_no_update BEFORE UPDATE ON execution_authority_events BEGIN SELECT RAISE(ABORT,'execution_authority_events is insert-only'); END;
		CREATE TRIGGER execution_authority_events_no_delete BEFORE DELETE ON execution_authority_events BEGIN SELECT RAISE(ABORT,'execution_authority_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_no_update BEFORE UPDATE ON strategy_selection_events BEGIN SELECT RAISE(ABORT,'strategy_selection_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_no_delete BEFORE DELETE ON strategy_selection_events BEGIN SELECT RAISE(ABORT,'strategy_selection_events is insert-only'); END;
		CREATE TRIGGER strategy_selection_events_state_guard BEFORE INSERT ON strategy_selection_events BEGIN
			SELECT CASE WHEN NEW.expected_current_event_id != COALESCE((SELECT event_id FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1),'no_event') THEN RAISE(ABORT,'strategy selection expected current event is stale') END;
			SELECT CASE WHEN NEW.previous_selected_result_sha256 != COALESCE((SELECT selected_result_sha256 FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1),'no_strategy') THEN RAISE(ABORT,'strategy selection previous result is stale') END;
			SELECT CASE WHEN NEW.event_type='SELECT' AND (NEW.selected_result_sha256 != NEW.candidate_result_sha256 OR NOT EXISTS(SELECT 1 FROM strategy_research_evidence WHERE result_sha256=NEW.candidate_result_sha256 AND target='paper_candidate')) THEN RAISE(ABORT,'strategy selection requires paper_candidate evidence') END;
			SELECT CASE WHEN NEW.event_type='ROLLBACK' AND NEW.source_event_id != COALESCE((SELECT event_id FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1),'no_event') THEN RAISE(ABORT,'strategy rollback source is stale') END;
			SELECT CASE WHEN NEW.paper_evaluation_sequence != COALESCE((SELECT MAX(sequence) FROM paper_evaluation_events),0) THEN RAISE(ABORT,'strategy selection paper evaluation sequence is stale') END;
		END;
		DELETE FROM schema_migrations WHERE version=20;`)
	if restoreErr := restoreForeignKeysForTest(db); err != nil || restoreErr != nil {
		t.Fatal(errors.Join(err, restoreErr))
	}
}

func downgradeLedgerReplayForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	dropBenchmarkSchemaForLegacy(t, db)
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='ledger_replay_state'`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present == 0 {
		return
	}
	if _, err := db.Exec(`DROP TRIGGER events_replay_policy_guard; DROP TABLE ledger_replay_state; DROP TABLE ledger_replay_transition;
        DROP TRIGGER events_cash_void_guard; DELETE FROM schema_migrations WHERE version=22`); err != nil {
		t.Fatal(err)
	}
	script, err := migrationFiles.ReadFile("migrations/009_fx_exchange.sql")
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

func privateLegacyMigrate(svc *Store) error {
	owners, err := mixedBackupOwners(svc.db.DB, svc.now, svc.id)
	if err != nil {
		return err
	}
	module, err := maintenance.Compose(maintenance.Config{DB: svc.db.DB, Observations: owners.Observations, Trading: owners.Trading, Now: svc.now, ID: svc.id, NewOwners: func(db *sql.DB) (maintenance.Owners, error) { return mixedBackupOwners(db, svc.now, svc.id) }})
	if err != nil {
		return err
	}
	return module.Migrations.Migrate(context.Background())
}
