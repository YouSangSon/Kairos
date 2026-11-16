package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestG38EPolicyRecordsNoActionDecisionsAndRejectsStaleEvidence(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name          string
		closes        []string
		wantDecision  string
		wantReason    string
		makeSelection bool
	}{
		{"insufficient", []string{"100"}, "INSUFFICIENT", "minimum_same_selection_samples_not_met", false},
		{"hold", []string{"100", "100"}, "HOLD", "within_local_paper_safety_bounds", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, performance := g38EPerformanceWindow(t, test.closes)
			before := g38EJournalCounts(t, svc)
			event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
				performance.StrategySelectionEventID, performance.StrategyPerformanceID)
			if err != nil || event.Decision != test.wantDecision || event.ReasonCode != test.wantReason ||
				event.AutomaticHaltCount != 0 || event.RollbackSelectionEventID != "" {
				t.Fatalf("event=%+v err=%v", event, err)
			}
			after := g38EJournalCounts(t, svc)
			if after.Policy != before.Policy+1 || after.Authority != before.Authority || after.Selection != before.Selection {
				t.Fatalf("no-action counts before=%+v after=%+v", before, after)
			}
		})
	}

	t.Run("stale and cross-selection evidence leave no policy event", func(t *testing.T) {
		svc, performance := g38EPerformanceWindow(t, []string{"100", "100"})
		before := g38EJournalCounts(t, svc)
		if _, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef, "strategy_selection_stale", performance.StrategyPerformanceID); err == nil {
			t.Fatal("stale selection was accepted")
		}
		artifact := rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) {
			result["experiment_id"] = "g38e-cross-selection"
		})
		evidence, err := svc.registerStrategyEvidence(ctx, artifact)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, performance.StrategySelectionEventID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
			performance.StrategySelectionEventID, performance.StrategyPerformanceID); err == nil {
			t.Fatal("superseded selection was accepted")
		}
		after := g38EJournalCounts(t, svc)
		if after.Policy != before.Policy || after.Authority != before.Authority || after.Selection != before.Selection+1 {
			t.Fatalf("stale side effects before=%+v after=%+v", before, after)
		}
	})
}

func TestG38EPolicyAtomicallyHaltsAllArmedAccountsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	secondAccount := "kiwoom_account_BBBBBBBBBBBBBBBBBBBBBBBB"
	if _, err := svc.SetSyntheticExecutionArmed(ctx, secondAccount, true); err != nil {
		t.Fatal(err)
	}
	before := g38EJournalCounts(t, svc)
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil || event.Decision != "HALT_AND_ROLLBACK" || event.ReasonCode != "max_drawdown_limit_reached" ||
		event.AutomaticHaltCount != 2 || event.RollbackSelectionEventID == "" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	after := g38EJournalCounts(t, svc)
	if after.Policy != before.Policy+1 || after.Authority != before.Authority+2 || after.Selection != before.Selection+1 {
		t.Fatalf("action counts before=%+v after=%+v", before, after)
	}
	rows, err := svc.db.Query(`SELECT account_ref,reason_code,paper_performance_policy_event_id
		FROM execution_authority_events WHERE paper_performance_policy_event_id=? ORDER BY sequence`, event.PolicyEventID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var accounts []string
	for rows.Next() {
		var account, reason, policyID string
		if err := rows.Scan(&account, &reason, &policyID); err != nil {
			t.Fatal(err)
		}
		if reason != "automatic_performance_halt" || policyID != event.PolicyEventID {
			t.Fatalf("automatic halt provenance account=%s reason=%s policy=%s", account, reason, policyID)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil || len(accounts) != 2 || accounts[0] >= accounts[1] {
		t.Fatalf("automatic halt lexical order=%v err=%v", accounts, err)
	}
	var rollbackReason, rollbackPolicyID string
	if err := svc.db.QueryRow(`SELECT reason_code,paper_performance_policy_event_id FROM strategy_selection_events WHERE event_id=?`,
		event.RollbackSelectionEventID).Scan(&rollbackReason, &rollbackPolicyID); err != nil ||
		rollbackReason != "automatic_performance_rollback" || rollbackPolicyID != event.PolicyEventID {
		t.Fatalf("rollback provenance reason=%s policy=%s err=%v", rollbackReason, rollbackPolicyID, err)
	}
	for _, account := range accounts {
		state, err := loadExecutionAuthoritySnapshot(ctx, svc.db, account)
		if err != nil || state.Armed || state.LeaseOwner != "" || state.LeaseExpiresAt != "" {
			t.Fatalf("halted authority account=%s state=%+v err=%v", account, state, err)
		}
	}
	state, err := replayStrategyRegistry(ctx, svc.db)
	if err != nil || state.CurrentEventID != event.RollbackSelectionEventID || state.SelectedResultSHA256 != noStrategySelection {
		t.Fatalf("rollback state=%+v err=%v", state, err)
	}
	retry, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil || retry.PolicyEventID != event.PolicyEventID || g38EJournalCounts(t, svc) != after {
		t.Fatalf("retry=%+v err=%v counts=%+v want=%+v", retry, err, g38EJournalCounts(t, svc), after)
	}
}

func TestG38EPolicyActionUsesOneProvenanceTime(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	base := mustTime("2026-01-16T07:00:00Z")
	calls := 0
	svc.now = func() time.Time {
		value := base.Add(time.Duration(calls) * time.Second)
		calls++
		return value
	}
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil || event.Decision != "HALT_AND_ROLLBACK" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	wantRecordedAt := base.Format(canonicalPaperTimeLayout)
	if calls != 1 || event.RecordedAt != wantRecordedAt {
		t.Fatalf("policy clock calls=%d recorded_at=%s want=%s", calls, event.RecordedAt, wantRecordedAt)
	}
	assertActionTime := func(name, recordedAt string, err error) {
		t.Helper()
		value, parseErr := time.Parse(time.RFC3339Nano, recordedAt)
		if err != nil || parseErr != nil || !value.Equal(base) {
			t.Fatalf("%s recorded_at=%s want instant=%s errors=(%v,%v)", name, recordedAt, wantRecordedAt, err, parseErr)
		}
	}
	rows, err := svc.db.Query(`SELECT recorded_at FROM execution_authority_events WHERE paper_performance_policy_event_id=?`, event.PolicyEventID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var recordedAt string
		if err := rows.Scan(&recordedAt); err != nil {
			t.Fatal(err)
		}
		assertActionTime("automatic halt", recordedAt, nil)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var rollbackRecordedAt string
	if err := svc.db.QueryRow(`SELECT recorded_at FROM strategy_selection_events WHERE event_id=?`, event.RollbackSelectionEventID).Scan(&rollbackRecordedAt); err != nil {
		t.Fatal(err)
	}
	assertActionTime("rollback", rollbackRecordedAt, nil)
}

func TestG38EPolicyRollsBackWithoutArmedAuthority(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	if _, err := svc.SetSyntheticExecutionArmed(ctx, k2aAccountRef, false); err != nil {
		t.Fatal(err)
	}
	before := g38EJournalCounts(t, svc)
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil || event.Decision != "HALT_AND_ROLLBACK" || event.AutomaticHaltCount != 0 || event.RollbackSelectionEventID == "" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	after := g38EJournalCounts(t, svc)
	if after.Policy != before.Policy+1 || after.Authority != before.Authority || after.Selection != before.Selection+1 {
		t.Fatalf("zero-armed action before=%+v after=%+v", before, after)
	}
}

func TestG38EPolicyActionFailureRollsBackEveryJournal(t *testing.T) {
	for _, target := range []string{"paper_performance_policy_events", "execution_authority_events", "strategy_selection_events"} {
		t.Run(target, func(t *testing.T) {
			svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
			before := g38EJournalCounts(t, svc)
			if _, err := svc.db.Exec(`CREATE TRIGGER g38e_forced_failure BEFORE INSERT ON ` + target +
				` BEGIN SELECT RAISE(ABORT, 'forced G3.8E failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ApplyPaperPerformancePolicy(context.Background(), k2aAccountRef,
				performance.StrategySelectionEventID, performance.StrategyPerformanceID); err == nil {
				t.Fatal("forced action failure was accepted")
			}
			if after := g38EJournalCounts(t, svc); after != before {
				t.Fatalf("partial action write before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestG38EPolicyConcurrentIdenticalCallsConverge(t *testing.T) {
	primary, performance := g38EPerformanceWindow(t, []string{"100", "100"})
	secondary := secondG38C2Service(t, primary, mustTime("2026-01-15T07:00:00Z"))
	start := make(chan struct{})
	results := make(chan *PaperPerformancePolicyEvent, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, svc := range []*Store{primary, secondary} {
		wg.Add(1)
		go func(svc *Store) {
			defer wg.Done()
			<-start
			event, err := svc.ApplyPaperPerformancePolicy(context.Background(), k2aAccountRef,
				performance.StrategySelectionEventID, performance.StrategyPerformanceID)
			results <- event
			errs <- err
		}(svc)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	var policyID string
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for event := range results {
		if event == nil || (policyID != "" && policyID != event.PolicyEventID) {
			t.Fatalf("concurrent policy results diverged: prior=%s event=%+v", policyID, event)
		}
		policyID = event.PolicyEventID
	}
	if g38EJournalCounts(t, primary).Policy != 1 {
		t.Fatalf("concurrent policy rows=%+v", g38EJournalCounts(t, primary))
	}
}

func TestG38EPolicyRootRecoveryRejectsTamperedPolicyAndNeverReturnsCorruptRetry(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := provePaperPerformancePolicyRecovery(ctx, svc.db)
	if err != nil || proof.Events != 1 || proof.Actions != 1 || proof.AutomaticHalts != event.AutomaticHaltCount || proof.SHA256 == "" {
		t.Fatalf("policy proof=%+v err=%v", proof, err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_performance_policy_events_no_update;
		UPDATE paper_performance_policy_events SET record_sha256=? WHERE policy_event_id=?`, emptySHA256, event.PolicyEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err == nil {
		t.Fatal("policy recovery certified a tampered policy row")
	}
	before := g38EJournalCounts(t, svc)
	if _, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID); err == nil {
		t.Fatal("corrupt policy retry was returned")
	}
	if after := g38EJournalCounts(t, svc); after != before {
		t.Fatalf("corrupt retry appended journal rows before=%+v after=%+v", before, after)
	}
}

func TestG38EPolicyUsesGlobalStrategyPerformanceCutoffAcrossAccounts(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	other := g38ELaterOtherAccountStrategyPerformance(t, svc, performance)
	var globalCutoff int64
	if err := svc.db.QueryRow(`SELECT MAX(sequence) FROM paper_strategy_performance_events`).Scan(&globalCutoff); err != nil {
		t.Fatal(err)
	}
	if globalCutoff <= 0 || other.StrategyPerformanceID == performance.StrategyPerformanceID {
		t.Fatalf("invalid cross-account G3.8D fixture cutoff=%d target=%s other=%s", globalCutoff, performance.StrategyPerformanceID, other.StrategyPerformanceID)
	}
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil || event.PaperStrategyPerformanceSequenceCutoff != globalCutoff {
		t.Fatalf("policy=%+v want global strategy-performance cutoff=%d err=%v", event, globalCutoff, err)
	}
	if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err != nil {
		t.Fatalf("cross-account global cutoff policy replay failed: %v", err)
	}
}

func TestG38EPolicyRootRecoveryRejectsCanonicalRecordedCutoffDowngrade(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	_ = g38ELaterOtherAccountStrategyPerformance(t, svc, performance)
	var globalCutoff, targetCutoff int64
	if err := svc.db.QueryRow(`SELECT MAX(sequence) FROM paper_strategy_performance_events`).Scan(&globalCutoff); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT sequence FROM paper_strategy_performance_events WHERE strategy_performance_id=?`, performance.StrategyPerformanceID).Scan(&targetCutoff); err != nil {
		t.Fatal(err)
	}
	if targetCutoff >= globalCutoff {
		t.Fatalf("fixture cutoff target=%d global=%d", targetCutoff, globalCutoff)
	}
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil || event.PaperStrategyPerformanceSequenceCutoff != globalCutoff {
		t.Fatalf("policy=%+v global cutoff=%d err=%v", event, globalCutoff, err)
	}
	mutated := *event
	mutated.PaperStrategyPerformanceSequenceCutoff = targetCutoff
	recordJSON, recordSHA, err := orderJSONHash(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_performance_policy_events_no_update;
		UPDATE paper_performance_policy_events
		SET paper_strategy_performance_sequence_cutoff=?,record_json=?,record_sha256=?
		WHERE policy_event_id=?`, targetCutoff, string(recordJSON), recordSHA, event.PolicyEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err == nil {
		t.Fatal("policy root accepted a canonical rewrite that downgraded the global strategy-performance cutoff")
	}
}

func TestG38EPolicyRootRecoveryBindsAutomaticActionProvenanceTime(t *testing.T) {
	ctx := context.Background()
	for name, mutate := range map[string]func(*testing.T, *Store, *PaperPerformancePolicyEvent){
		"halt": func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
			var record executionAuthorityRecord
			var raw string
			if err := svc.db.QueryRow(`SELECT record_json FROM execution_authority_events WHERE paper_performance_policy_event_id=? ORDER BY sequence LIMIT 1`, event.PolicyEventID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatal(err)
			}
			record.RecordedAt = "2026-01-16T07:00:01Z"
			canonical, hash, err := orderJSONHash(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.db.Exec(`DROP TRIGGER execution_authority_events_no_update;
				UPDATE execution_authority_events SET record_sha256=?,record_json=?,recorded_at=? WHERE event_id=?`, hash, string(canonical), record.RecordedAt, record.EventID); err != nil {
				t.Fatal(err)
			}
		},
		"rollback": func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
			var record StrategySelectionEvent
			var raw string
			if err := svc.db.QueryRow(`SELECT record_json FROM strategy_selection_events WHERE event_id=?`, event.RollbackSelectionEventID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatal(err)
			}
			record.RecordedAt = "2026-01-16T07:00:01Z"
			canonical, hash, err := orderJSONHash(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.db.Exec(`DROP TRIGGER strategy_selection_events_no_update;
				UPDATE strategy_selection_events SET record_sha256=?,record_json=?,recorded_at=? WHERE event_id=?`, hash, string(canonical), record.RecordedAt, record.EventID); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
			event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
				performance.StrategySelectionEventID, performance.StrategyPerformanceID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err != nil {
				t.Fatalf("baseline policy proof failed: %v", err)
			}
			mutate(t, svc, event)
			if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err == nil {
				t.Fatal("policy root accepted a canonically rehashed action with a different provenance instant")
			}
		})
	}
}

func TestG38EPolicyRootRejectsCanonicalMutationMatrix(t *testing.T) {
	ctx := context.Background()
	secondAccount := "kiwoom_account_BBBBBBBBBBBBBBBBBBBBBBBB"
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *Store, *PaperPerformancePolicyEvent)
	}{
		{
			name: "decision and reason",
			mutate: func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
				mutated := *event
				mutated.Decision = "HOLD"
				mutated.ReasonCode = "within_local_paper_safety_bounds"
				mutated.RollbackSelectionEventID = ""
				mutated.AutomaticHaltCount = 0
				g38ERewritePolicyEvent(t, svc, mutated)
			},
		},
		{
			name: "latest G3.8D source link",
			mutate: func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
				mutated := *event
				mutated.LatestPerformanceID = mutated.BaselinePerformanceID
				g38ERewritePolicyEvent(t, svc, mutated)
			},
		},
		{
			name: "sample count",
			mutate: func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
				mutated := *event
				mutated.SampleCount++
				g38ERewritePolicyEvent(t, svc, mutated)
			},
		},
		{
			name: "authority fencing token",
			mutate: func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
				var raw string
				if err := svc.db.QueryRow(`SELECT record_json FROM execution_authority_events
					WHERE paper_performance_policy_event_id=? ORDER BY sequence LIMIT 1`, event.PolicyEventID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var record executionAuthorityRecord
				if err := json.Unmarshal([]byte(raw), &record); err != nil {
					t.Fatal(err)
				}
				record.FencingToken += 10
				canonical, hash, err := orderJSONHash(record)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := svc.db.Exec(`DROP TRIGGER execution_authority_events_no_update;
					UPDATE execution_authority_events SET fencing_token=?,record_sha256=?,record_json=? WHERE event_id=?`,
					record.FencingToken, hash, string(canonical), record.EventID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "automatic halt coverage",
			mutate: func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
				mutated := *event
				mutated.AutomaticHaltCount--
				g38ERewritePolicyEvent(t, svc, mutated)
			},
		},
		{
			name: "automatic rollback source",
			mutate: func(t *testing.T, svc *Store, event *PaperPerformancePolicyEvent) {
				var raw string
				if err := svc.db.QueryRow(`SELECT record_json FROM strategy_selection_events WHERE event_id=?`, event.RollbackSelectionEventID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var record StrategySelectionEvent
				if err := json.Unmarshal([]byte(raw), &record); err != nil {
					t.Fatal(err)
				}
				record.SourceEventID = record.EventID
				canonical, hash, err := orderJSONHash(record)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := svc.db.Exec(`DROP TRIGGER strategy_selection_events_no_update;
					UPDATE strategy_selection_events SET source_event_id=?,record_sha256=?,record_json=? WHERE event_id=?`,
					record.SourceEventID, hash, string(canonical), record.EventID); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
			if _, err := svc.SetSyntheticExecutionArmed(ctx, secondAccount, true); err != nil {
				t.Fatal(err)
			}
			event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
				performance.StrategySelectionEventID, performance.StrategyPerformanceID)
			if err != nil || event.Decision != "HALT_AND_ROLLBACK" || event.AutomaticHaltCount != 2 {
				t.Fatalf("policy=%+v err=%v", event, err)
			}
			if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err != nil {
				t.Fatalf("baseline policy recovery failed: %v", err)
			}
			test.mutate(t, svc, event)
			if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err == nil {
				t.Fatal("policy root accepted a canonically rehashed mutation")
			}
		})
	}
}

func g38ERewritePolicyEvent(t testing.TB, svc *Store, event PaperPerformancePolicyEvent) {
	t.Helper()
	canonical, hash, err := orderJSONHash(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_performance_policy_events_no_update;
		UPDATE paper_performance_policy_events SET
		schema_version=?,policy_version=?,account_ref=?,paper_accounting_session_id=?,strategy_selection_event_id=?,
		selected_strategy_result_ref=?,strategy_performance_id=?,baseline_performance_id=?,latest_performance_id=?,sample_count=?,
		expected_previous_policy_event_id=?,strategy_selection_sequence_cutoff=?,paper_strategy_performance_sequence_cutoff=?,
		execution_authority_sequence_cutoff=?,decision=?,reason_code=?,rollback_selection_event_id=?,automatic_halt_count=?,
		record_sha256=?,record_json=?,recorded_at=? WHERE policy_event_id=?`,
		event.SchemaVersion, event.PolicyVersion, event.AccountRef, event.PaperAccountingSessionID, event.StrategySelectionEventID,
		event.SelectedStrategyResultRef, event.StrategyPerformanceID, event.BaselinePerformanceID, event.LatestPerformanceID, event.SampleCount,
		event.ExpectedPreviousPolicyEventID, event.StrategySelectionSequenceCutoff, event.PaperStrategyPerformanceSequenceCutoff,
		event.ExecutionAuthoritySequenceCutoff, event.Decision, event.ReasonCode, nullable(event.RollbackSelectionEventID), event.AutomaticHaltCount,
		hash, string(canonical), event.RecordedAt, event.PolicyEventID); err != nil {
		t.Fatal(err)
	}
}

func TestG38ESyntheticPublicWritersRejectTamperedAutomaticPolicy(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	order := mustRecordK2AOrder(t, svc, "g38e-corrupt-policy-dispatch")
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER paper_performance_policy_events_no_update;
		UPDATE paper_performance_policy_events SET record_sha256=? WHERE policy_event_id=?`, emptySHA256, event.PolicyEventID); err != nil {
		t.Fatal(err)
	}
	before := g38EJournalCounts(t, svc)
	var reservationsBefore, ordersBefore int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM risk_reservations`).Scan(&reservationsBefore); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_idempotency`).Scan(&ordersBefore); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.authorizeSyntheticDispatchOnce(ctx, order.OrderID, 1); err == nil ||
		!strings.Contains(err.Error(), "execution authority recovery") {
		t.Fatalf("synthetic dispatch did not fail closed on corrupt automatic policy: %v", err)
	}
	if _, err := svc.recordPaperTarget(ctx, k2aAccountRef, PaperSignal{}, "", nil, time.Time{}, time.Time{}, time.Time{}, 0); err == nil ||
		!strings.Contains(err.Error(), "execution authority recovery") {
		t.Fatalf("paper target did not fail closed on corrupt automatic policy: %v", err)
	}
	var reservationsAfter, ordersAfter int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM risk_reservations`).Scan(&reservationsAfter); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_idempotency`).Scan(&ordersAfter); err != nil {
		t.Fatal(err)
	}
	if after := g38EJournalCounts(t, svc); after != before || reservationsAfter != reservationsBefore || ordersAfter != ordersBefore {
		t.Fatalf("corrupt synthetic writer appended rows journal=%+v/%+v reservations=%d/%d orders=%d/%d",
			before, after, reservationsBefore, reservationsAfter, ordersBefore, ordersAfter)
	}
}

func TestG38EAutomaticHaltIDsAreDerivedFromPolicyAndAccount(t *testing.T) {
	ctx := context.Background()
	svc, performance := g38EPerformanceWindow(t, []string{"100", "10"})
	secondAccount := "kiwoom_account_BBBBBBBBBBBBBBBBBBBBBBBB"
	if _, err := svc.SetSyntheticExecutionArmed(ctx, secondAccount, true); err != nil {
		t.Fatal(err)
	}
	event, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		performance.StrategySelectionEventID, performance.StrategyPerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.db.Query(`SELECT account_ref,event_id FROM execution_authority_events
		WHERE paper_performance_policy_event_id=? ORDER BY account_ref`, event.PolicyEventID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var accountRef, eventID string
		if err := rows.Scan(&accountRef, &eventID); err != nil {
			t.Fatal(err)
		}
		if want := paperPerformanceAutomaticHaltID(event.PolicyEventID, accountRef); eventID != want {
			t.Fatalf("automatic halt ID=%q want %q", eventID, want)
		}
		count++
	}
	if err := rows.Err(); err != nil || count != event.AutomaticHaltCount {
		t.Fatalf("automatic halt count=%d/%d err=%v", count, event.AutomaticHaltCount, err)
	}
}

func TestG38EAuthorityRecoveryRejectsRehashedAutomaticHaltRelinkToOtherValidPolicy(t *testing.T) {
	ctx := context.Background()
	svc, firstPerformance := g38EPerformanceWindow(t, []string{"100", "10"})
	firstPolicy, err := svc.ApplyPaperPerformancePolicy(ctx, k2aAccountRef,
		firstPerformance.StrategySelectionEventID, firstPerformance.StrategyPerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	artifact := rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) {
		result["experiment_id"] = "g38e-other-valid-halt-policy"
		result["execution"].(map[string]any)["starting_cash"] = "1000"
	})
	evidence, err := svc.registerStrategyEvidence(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	strategy, err := replayStrategyRegistry(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, strategy.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	secondAccount := "kiwoom_account_BBBBBBBBBBBBBBBBBBBBBBBB"
	firstSecondAccountPerformance := g38EOtherAccountStrategyPerformanceAt(t, svc, &PaperStrategyPerformanceEvent{
		SelectedStrategyResultRef: evidence.ResultSHA256, StrategySelectionEventID: selection.CurrentEventID,
	}, mustTime("2026-01-16T00:00:00Z"))
	svc.now = func() time.Time { return mustTime("2026-01-19T07:00:00Z") }
	const asOf = "2026-01-19T06:30:00.000000000Z"
	recordG38C3MarkBar(t, svc, "005930", "g38e-other-valid-halt-policy-mark", asOf, "10")
	point, err := svc.EvaluatePaperPerformance(ctx, secondAccount, asOf)
	if err != nil {
		t.Fatal(err)
	}
	secondPerformance, err := svc.EvaluatePaperStrategyPerformance(ctx, secondAccount, point.StrategySelectionEventID, point.PerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	if firstSecondAccountPerformance.StrategySelectionEventID != secondPerformance.StrategySelectionEventID {
		t.Fatal("second policy fixture changed strategy selection")
	}
	secondPolicy, err := svc.ApplyPaperPerformancePolicy(ctx, secondAccount,
		secondPerformance.StrategySelectionEventID, secondPerformance.StrategyPerformanceID)
	if err != nil || secondPolicy.Decision != "HALT_AND_ROLLBACK" {
		t.Fatalf("second policy=%+v err=%v", secondPolicy, err)
	}
	var raw string
	if err := svc.db.QueryRow(`SELECT record_json FROM execution_authority_events
		WHERE paper_performance_policy_event_id=? AND account_ref=?`, secondPolicy.PolicyEventID, secondAccount).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var record executionAuthorityRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatal(err)
	}
	record.PaperPerformancePolicyEventID = firstPolicy.PolicyEventID
	canonical, hash, err := orderJSONHash(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER execution_authority_events_no_update;
		UPDATE execution_authority_events SET paper_performance_policy_event_id=?,record_sha256=?,record_json=? WHERE event_id=?`,
		firstPolicy.PolicyEventID, hash, string(canonical), record.EventID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := proveExecutionAuthorityRecovery(ctx, svc.db); err == nil {
		t.Fatal("authority replay accepted an automatic halt re-linked to another valid HALT policy")
	}
}

type g38ECounts struct{ Policy, Authority, Selection int }

func g38EJournalCounts(t testing.TB, svc *Store) g38ECounts {
	t.Helper()
	var result g38ECounts
	for query, destination := range map[string]*int{
		`SELECT COUNT(*) FROM paper_performance_policy_events`: &result.Policy,
		`SELECT COUNT(*) FROM execution_authority_events`:      &result.Authority,
		`SELECT COUNT(*) FROM strategy_selection_events`:       &result.Selection,
	} {
		if err := svc.db.QueryRow(query).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func g38EPerformanceWindow(t *testing.T, closes []string) (*Store, *PaperStrategyPerformanceEvent) {
	t.Helper()
	svc, _ := testService(t, nil, nil)
	ctx := context.Background()
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:00Z") }
	artifact := rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(result map[string]any) {
		result["experiment_id"] = "g38e-policy-window"
		result["execution"].(map[string]any)["starting_cash"] = "1000"
	})
	evidence, err := svc.registerStrategyEvidence(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	signalBar, err := svc.RecordPaperMarketBar(ctx, g38c2PaperMarketBar("g38e-policy-signal", "2026-01-09T00:00:00Z", "2026-01-09T06:30:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	signal := PaperSignal{
		SchemaVersion: capitalizedPaperSignalSchema, SignalID: "g38e-policy-signal", SignalBarObservationID: signalBar.ObservationID,
		StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID,
		DataSHA256: signalBar.InputDataSHA256, Symbol: signalBar.Symbol, TargetQuantity: "2", DataAsOf: signalBar.CloseAt,
		GeneratedAt: signalBar.SourceAvailableAt, ExpiresAt: "2026-01-20T00:00:00.000000000Z",
	}
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, order, err := svc.AdmitPaperSignal(ctx, k2aAccountRef, signal, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	recordG38C2FillBar(t, svc, "g38e-policy-fill", "100", "5", "2026-01-10")
	if state, err := svc.RunPaperOrder(ctx, order.OrderID, lease.FencingToken); err != nil || state.Status != "FILLED" {
		t.Fatalf("filled state=%+v err=%v", state, err)
	}
	svc.now = func() time.Time { return mustTime("2026-01-15T07:00:00Z") }
	var latest *PaperStrategyPerformanceEvent
	for index, close := range closes {
		asOf := time.Date(2026, time.January, 11+index, 6, 30, 0, 0, time.UTC).Format(canonicalPaperTimeLayout)
		recordG38C3MarkBar(t, svc, "005930", "g38e-policy-mark-"+close+time.Date(2026, time.January, 11+index, 0, 0, 0, 0, time.UTC).Format("20060102"), asOf, close)
		point, err := svc.EvaluatePaperPerformance(ctx, k2aAccountRef, asOf)
		if err != nil {
			t.Fatal(err)
		}
		latest, err = svc.EvaluatePaperStrategyPerformance(ctx, k2aAccountRef, point.StrategySelectionEventID, point.PerformanceID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if latest == nil {
		t.Fatal("G3.8E policy fixture needs at least one strategy performance point")
	}
	return svc, latest
}

func g38ELaterOtherAccountStrategyPerformance(t *testing.T, svc *Store, primary *PaperStrategyPerformanceEvent) *PaperStrategyPerformanceEvent {
	return g38EOtherAccountStrategyPerformanceAt(t, svc, primary, mustTime("2026-01-13T00:00:00Z"))
}

func g38EOtherAccountStrategyPerformanceAt(t *testing.T, svc *Store, primary *PaperStrategyPerformanceEvent, start time.Time) *PaperStrategyPerformanceEvent {
	t.Helper()
	ctx := context.Background()
	const account = "kiwoom_account_BBBBBBBBBBBBBBBBBBBBBBBB"
	signalDay := start.UTC()
	fillDay := signalDay.AddDate(0, 0, 1)
	markDay := signalDay.AddDate(0, 0, 2)
	svc.now = func() time.Time { return signalDay.Add(7 * time.Hour) }
	if _, err := svc.OpenPaperAccountingSession(ctx, account, primary.SelectedStrategyResultRef, primary.StrategySelectionEventID); err != nil {
		t.Fatal(err)
	}
	signalInput := g38c2PaperMarketBar("g38e-global-cutoff-signal", signalDay.Format("2006-01-02T15:04:05Z"), signalDay.Add(6*time.Hour+30*time.Minute).Format("2006-01-02T15:04:05Z"))
	signalInput.SourceAvailableAt, signalInput.FetchedAt = signalDay.Add(6*time.Hour+31*time.Minute).Format("2006-01-02T15:04:05Z"), signalDay.Add(6*time.Hour+32*time.Minute).Format("2006-01-02T15:04:05Z")
	signalBar, err := svc.RecordPaperMarketBar(ctx, signalInput)
	if err != nil {
		t.Fatal(err)
	}
	signal := PaperSignal{
		SchemaVersion: capitalizedPaperSignalSchema, SignalID: "g38e-global-cutoff-signal", SignalBarObservationID: signalBar.ObservationID,
		StrategyResultSHA256: primary.SelectedStrategyResultRef, StrategySelectionEventID: primary.StrategySelectionEventID,
		DataSHA256: signalBar.InputDataSHA256, Symbol: signalBar.Symbol, TargetQuantity: "2", DataAsOf: signalBar.CloseAt,
		GeneratedAt: signalBar.SourceAvailableAt, ExpiresAt: "2026-01-20T00:00:00.000000000Z",
	}
	lease := mustK2CLease(t, svc, account)
	_, order, err := svc.AdmitPaperSignal(ctx, account, signal, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return fillDay.Add(7 * time.Hour) }
	fillInput := g38c2PaperMarketBar("g38e-global-cutoff-fill", fillDay.Format("2006-01-02T15:04:05Z"), fillDay.Add(6*time.Hour+30*time.Minute).Format("2006-01-02T15:04:05Z"))
	fillInput.Open, fillInput.High, fillInput.Low, fillInput.Close, fillInput.Volume = "100", "100", "100", "100", "5"
	fillInput.SourceAvailableAt, fillInput.FetchedAt = fillDay.Add(6*time.Hour+31*time.Minute).Format("2006-01-02T15:04:05Z"), fillDay.Add(6*time.Hour+32*time.Minute).Format("2006-01-02T15:04:05Z")
	if _, err := svc.RecordPaperMarketBar(ctx, fillInput); err != nil {
		t.Fatal(err)
	}
	lease = mustK2CLease(t, svc, account)
	if state, err := svc.RunPaperOrder(ctx, order.OrderID, lease.FencingToken); err != nil || state.Status != "FILLED" {
		t.Fatalf("cross-account filled state=%+v err=%v", state, err)
	}
	svc.now = func() time.Time { return markDay.Add(7 * time.Hour) }
	asOf := markDay.Add(6*time.Hour + 30*time.Minute).Format(canonicalPaperTimeLayout)
	recordG38C3MarkBar(t, svc, "005930", "g38e-global-cutoff-mark", asOf, "100")
	point, err := svc.EvaluatePaperPerformance(ctx, account, asOf)
	if err != nil {
		t.Fatal(err)
	}
	event, err := svc.EvaluatePaperStrategyPerformance(ctx, account, point.StrategySelectionEventID, point.PerformanceID)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
