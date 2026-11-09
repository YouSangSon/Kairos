package sqlite

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type paperPromotionFixture struct {
	svc        *Store
	dbPath     string
	incumbent  *StrategyEvidence
	challenger *StrategyEvidence
	selection  *StrategySelectionState
	session    *PaperAccountingSession
	bars       []byte
	barsPath   string
}

func newPaperPromotionFixture(t *testing.T) paperPromotionFixture {
	t.Helper()
	svc, dbPath := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-10-27T15:00:00Z") }
	ctx := context.Background()
	artifact := func(fast, slow int) []byte {
		return strategyArtifact(t, func(config map[string]any) {
			strategy := config["strategy"].(map[string]any)
			strategy["fast_windows"], strategy["slow_windows"] = []int{fast}, []int{slow}
		})
	}
	incumbent, err := svc.registerStrategyEvidence(ctx, artifact(3, 6))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(ctx, incumbent.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.OpenPaperAccountingSession(ctx, k2aAccountRef, incumbent.ResultSHA256, selected.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	challenger, err := svc.registerStrategyEvidence(ctx, artifact(2, 5))
	if err != nil {
		t.Fatal(err)
	}
	bars := paperPromotionForwardBars(t, mustTime("2026-05-01T00:00:00Z"), 24*time.Hour)
	barsPath := filepath.Join(t.TempDir(), "forward.csv")
	if err := os.WriteFile(barsPath, bars, 0600); err != nil {
		t.Fatal(err)
	}
	return paperPromotionFixture{svc, dbPath, incumbent, challenger, selected, session, bars, barsPath}
}

func paperPromotionForwardBars(t *testing.T, start time.Time, step time.Duration) []byte {
	t.Helper()
	var input strings.Builder
	writer := csv.NewWriter(&input)
	if err := writer.Write([]string{"bar_at", "symbol", "open", "high", "low", "close", "volume"}); err != nil {
		t.Fatal(err)
	}
	closes := []int{20, 20, 20, 20, 20, 20}
	for fold := 0; fold < 3; fold++ {
		for price := 20; price >= 9; price-- {
			closes = append(closes, price)
		}
		for price := 10; price <= 27; price++ {
			closes = append(closes, price)
		}
	}
	for index, close := range closes {
		at := start.Add(time.Duration(index) * step).Format(time.RFC3339)
		if err := writer.Write([]string{at, "005930", strconv.Itoa(close), strconv.Itoa(close + 1), strconv.Itoa(close - 1), strconv.Itoa(close), "100"}); err != nil {
			t.Fatal(err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	return []byte(input.String())
}
