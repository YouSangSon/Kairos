package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func localPaperRestartFixture(t *testing.T) (*Store, string, []byte, *StrategyEvidence, *StrategySelectionState, []string) {
	t.Helper()
	svc, dbPath := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-05-01T07:00:00Z") }
	research, err := os.ReadFile(fixturePath("strategy-market-bars.csv"))
	if err != nil {
		t.Fatal(err)
	}
	research = []byte(strings.ReplaceAll(string(research), ",SMA,", ",005930,"))
	researchSHA := fmt.Sprintf("%x", sha256.Sum256(research))
	evidence, err := svc.registerStrategyEvidence(context.Background(), rehashedStrategyArtifact(t, strategyArtifact(t, nil), func(a map[string]any) {
		a["input_sha256"] = researchSHA
		a["manifest"].(map[string]any)["data"].(map[string]any)["input_sha256"] = researchSHA
	}))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(context.Background(), evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenPaperAccountingSession(context.Background(), k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	var artifact map[string]any
	if err := json.Unmarshal([]byte(evidence.ArtifactJSON), &artifact); err != nil {
		t.Fatal(err)
	}
	slow := int(artifact["manifest"].(map[string]any)["strategy"].(map[string]any)["parameters"].(map[string]any)["slow_window"].(float64))
	rows := make([]string, 0, slow+1)
	for i := slow; i >= 0; i-- {
		day := mustTime("2026-05-09T00:00:00Z").AddDate(0, 0, -i).Format("2006-01-02")
		price := "99"
		if i == 0 {
			price = "100"
		}
		rows = append(rows, localPaperRestartRow(day, price, price, "100"))
	}
	svc.now = func() time.Time { return mustTime("2026-05-10T07:00:00Z") }
	return svc, dbPath, research, evidence, selected, rows
}

func localPaperRestartRow(day, open, close, volume string) string {
	return fmt.Sprintf("%sT06:30:00Z,005930,KRX,Asia/Seoul,1d,%s,%s,%s,%s,%s,%sT00:00:00Z,%sT06:31:00Z,%sT06:32:00Z", day, open, open, open, close, volume, day, day, day)
}

func localPaperRestartProposal(t *testing.T, svc *Store, resultSHA, row string, raw []byte, kind string) []byte {
	t.Helper()
	dataAsOf := strings.SplitN(row, ",", 2)[0]
	signal := PaperSignal{StrategyResultSHA256: resultSHA, Symbol: "005930", DataAsOf: dataAsOf, DataSHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}
	return paperProposalForTest(t, svc, signal, kind)
}
