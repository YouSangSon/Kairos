package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPaperPromotionClaimRetryAbortAndConsumedRange(t *testing.T) {
	f := newPaperPromotionFixture(t)
	ctx := context.Background()
	const attempt = "promotion-pending"
	claim, prior, err := f.svc.claimPaperPromotion(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, f.bars)
	if err != nil || claim == nil || prior != nil {
		t.Fatalf("claim: %+v %+v %v", claim, prior, err)
	}
	status, err := f.svc.PaperPromotionStatus(ctx, attempt)
	if err != nil || status == nil || status.Decision != "PENDING" {
		t.Fatalf("pending status: %+v %v", status, err)
	}
	retry, err := f.svc.PromotePaperCandidate(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, f.bars)
	if err != nil || !reflect.DeepEqual(retry, status) {
		t.Fatalf("pending retry scored or changed receipt: %+v %v", retry, err)
	}
	changed := []byte(strings.ReplaceAll(string(f.bars), ",100\n", ",101\n"))
	if bytes.Equal(changed, f.bars) {
		t.Fatal("changed payload fixture did not change")
	}
	if _, err := f.svc.PromotePaperCandidate(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, changed); err == nil {
		t.Fatal("changed payload reused an attempt ID")
	}
	aborted, err := f.svc.AbortPaperPromotion(ctx, attempt)
	if err != nil || aborted == nil || aborted.Decision != "ABORTED" {
		t.Fatalf("abort: %+v %v", aborted, err)
	}
	retry, err = f.svc.PromotePaperCandidate(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, f.bars)
	if err != nil || !reflect.DeepEqual(retry, aborted) {
		t.Fatalf("aborted retry changed receipt: %+v %v", retry, err)
	}
	if _, _, err := f.svc.claimPaperPromotion(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, "overlap-other-bytes", changed); err == nil || !strings.Contains(err.Error(), "consumed") {
		t.Fatalf("aborted comparison interval was reused: %v", err)
	}
	fresh := paperPromotionForwardBars(t, mustTime("2026-08-05T00:00:00Z"), 12*time.Hour)
	second, prior, err := f.svc.claimPaperPromotion(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, "fresh-interval", fresh)
	if err != nil || second == nil || prior != nil {
		t.Fatalf("fresh comparison rejected: %+v %+v %v", second, prior, err)
	}
	if _, err := f.svc.AbortPaperPromotion(ctx, second.AttemptID); err != nil {
		t.Fatal(err)
	}
	proof, err := provePaperPromotionRecovery(ctx, f.svc.db)
	state, stateErr := replayStrategyRegistry(ctx, f.svc.db)
	if err != nil || stateErr != nil || proof.Claims != 2 || proof.Decisions != 2 || state.CurrentEventID != f.selection.CurrentEventID {
		t.Fatalf("claim lifecycle changed registry or journal: %+v %+v %v %v", proof, state, err, stateErr)
	}
}

func TestPaperPromotionFinishRejectsChangedSelectionOrArmedAccount(t *testing.T) {
	for _, change := range []string{"selection", "arm"} {
		t.Run(change, func(t *testing.T) {
			f := newPaperPromotionFixture(t)
			ctx := context.Background()
			claim, prior, err := f.svc.claimPaperPromotion(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, "stale-attempt", f.bars)
			if err != nil || claim == nil || prior != nil {
				t.Fatalf("claim: %+v %+v %v", claim, prior, err)
			}
			comparison, err := scorePaperPromotionClaim(ctx, f.svc.db, *claim)
			if err != nil {
				t.Fatal(err)
			}
			expectedSelection := f.selection.CurrentEventID
			if change == "selection" {
				state, err := f.svc.SelectPaperCandidate(ctx, f.challenger.ResultSHA256, expectedSelection)
				if err != nil || state == nil {
					t.Fatal(err)
				}
				expectedSelection = state.CurrentEventID
			} else if _, err := f.svc.SetSyntheticExecutionArmed(ctx, k2aAccountRef, true); err != nil {
				t.Fatal(err)
			}
			receipt, err := f.svc.finishPaperPromotion(ctx, *claim, comparison, nil, false)
			if err != nil || receipt == nil || receipt.Decision != "STALE" || receipt.SelectionEventID != "" {
				t.Fatalf("changed state did not stale comparison: %+v %v", receipt, err)
			}
			state, err := replayStrategyRegistry(ctx, f.svc.db)
			var automatic int
			queryErr := f.svc.db.QueryRow(`SELECT COUNT(*) FROM strategy_selection_events WHERE reason_code='auto_comparison'`).Scan(&automatic)
			if err != nil || queryErr != nil || automatic != 0 || state.CurrentEventID != expectedSelection {
				t.Fatalf("stale finish changed selection: %+v auto=%d err=%v/%v", state, automatic, err, queryErr)
			}
		})
	}
}

func checkPaperPromotionSignalAdmission(t *testing.T, f paperPromotionFixture, selection string, end time.Time, bars []*PaperMarketBarObservation) []*PaperMarketBarObservation {
	t.Helper()
	ctx := context.Background()
	createBars := bars == nil
	for i, offset := range []time.Duration{-time.Second, 0, time.Second} {
		closeAt := end.Add(offset)
		if err := validatePaperPromotionSignalCutoff(ctx, f.svc.db, f.challenger.ResultSHA256, selection, closeAt.Format(time.RFC3339)); (err == nil) != (offset > 0) {
			t.Fatalf("metadata cutoff offset %s: %v", offset, err)
		}
		if createBars {
			input := g38c2PaperMarketBar(fmt.Sprintf("promotion-bar-%d", i), closeAt.Add(-time.Hour).Format(time.RFC3339), closeAt.Format(time.RFC3339))
			input.SourceAvailableAt, input.FetchedAt = closeAt.Add(time.Minute).Format(time.RFC3339), closeAt.Add(2*time.Minute).Format(time.RFC3339)
			bar, err := f.svc.RecordPaperMarketBar(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			bars = append(bars, bar)
		}
		bar := bars[i]
		signal := PaperSignal{SchemaVersion: capitalizedPaperSignalSchema, SignalID: fmt.Sprintf("promotion-%s-%d", selection, i),
			SignalBarObservationID: bar.ObservationID, StrategyResultSHA256: f.challenger.ResultSHA256,
			StrategySelectionEventID: selection, DataSHA256: bar.InputDataSHA256, Symbol: bar.Symbol, TargetQuantity: "0",
			DataAsOf: bar.CloseAt, GeneratedAt: f.svc.now().Format(time.RFC3339), ExpiresAt: f.svc.now().Add(24 * time.Hour).Format(time.RFC3339)}
		before := g38c2PaperSignalSideEffects(t, f.svc)
		_, err := recordG38C2PaperSignalForTest(f.svc, k2aAccountRef, signal)
		if offset <= 0 && (err == nil || !strings.Contains(err.Error(), "comparison interval")) || offset > 0 && err != nil {
			t.Fatalf("signal cutoff offset %s: %v", offset, err)
		}
		if offset <= 0 {
			assertG38C2PaperSignalSideEffects(t, before, g38c2PaperSignalSideEffects(t, f.svc))
		}
	}
	return bars
}

func TestPaperPromotionRecoveryRejectsRehashedComparisonForgery(t *testing.T) {
	f := newPaperPromotionFixture(t)
	ctx := context.Background()
	const attempt = "promotion-forgery"
	receipt, err := f.svc.PromotePaperCandidate(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, f.bars)
	if err != nil || receipt == nil || receipt.Decision != "PROMOTE" {
		t.Fatalf("promotion: %+v %v", receipt, err)
	}
	var raw, trigger string
	if err := f.svc.db.QueryRow(`SELECT record_json FROM paper_promotion_decisions WHERE attempt_id=?`, attempt).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='paper_promotion_decisions_no_update'`).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	var decision paperPromotionDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil || decision.Comparison == nil || len(decision.Comparison.Windows) == 0 {
		t.Fatalf("invalid decision fixture: %v", err)
	}
	decision.Comparison.Windows[0].Candidate.AfterCostReturn = "999"
	canonical, sha, err := orderJSONHash(decision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.db.Exec(`DROP TRIGGER paper_promotion_decisions_no_update`); err != nil {
		t.Fatal(err)
	}
	_, updateErr := f.svc.db.Exec(`UPDATE paper_promotion_decisions SET record_json=?,record_sha256=? WHERE attempt_id=?`, string(canonical), sha, attempt)
	_, triggerErr := f.svc.db.Exec(trigger)
	if updateErr != nil || triggerErr != nil {
		t.Fatalf("forge fixture or restore trigger: %v / %v", updateErr, triggerErr)
	}
	if _, err := replayStrategyRegistry(ctx, f.svc.db); err == nil {
		t.Fatal("registry accepted a rehashed forged comparison")
	}
}
