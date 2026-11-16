package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func paperProposalFixture(t *testing.T) (*Service, PaperSignal, *PaperMarketBarObservation) {
	t.Helper()
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:00Z") }
	evidence, selected := selectedPaperStrategy(t, svc)
	if _, err := svc.openPaperAccountingSession(context.Background(), k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	var artifactJSON string
	if err := svc.db.QueryRow(`SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, evidence.ResultSHA256).Scan(&artifactJSON); err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(artifactJSON), &root); err != nil {
		t.Fatal(err)
	}
	params := root["manifest"].(map[string]any)["strategy"].(map[string]any)["parameters"].(map[string]any)
	slow := int(params["slow_window"].(float64))
	last := mustTime("2026-01-09T00:00:00Z")
	for i := slow; i > 0; i-- {
		day := last.AddDate(0, 0, -i).Format("2006-01-02")
		recordG38C2FillBar(t, svc, fmt.Sprintf("proposal-warmup-%d", i), "99", "100", day)
	}
	bar := recordG38C2FillBar(t, svc, "proposal-signal-bar", "100", "100", "2026-01-09")
	return svc, PaperSignal{SchemaVersion: capitalizedPaperSignalSchema, SignalID: "proposal-fixture", SignalBarObservationID: bar.ObservationID, StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID, DataSHA256: bar.InputDataSHA256, Symbol: bar.Symbol, DataAsOf: bar.CloseAt}, bar
}

func paperProposalForTest(t *testing.T, svc *Service, signal PaperSignal, kind string) []byte {
	t.Helper()
	var artifact string
	if err := svc.db.QueryRow(`SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, signal.StrategyResultSHA256).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	evidence, err := svc.Trading.RegisterEvidence(context.Background(), []byte(artifact))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(artifact), &root); err != nil {
		t.Fatal(err)
	}
	parameters := root["manifest"].(map[string]any)["strategy"].(map[string]any)["parameters"].(map[string]any)
	var quantity any = parameters["quantity"]
	if kind == "death_cross" {
		quantity = "0"
	}
	if kind == "none" {
		quantity = nil
	}
	body := map[string]any{
		"schema_version": "paper-signal-proposal.v1", "mode": "paper_proposal_only",
		"strategy_result_sha256": evidence.ResultSHA256, "strategy_parameter_sha256": evidence.ParameterSHA256,
		"input_sha256": signal.DataSHA256, "symbol": signal.Symbol, "data_as_of": strings.Replace(signal.DataAsOf, ".000000000Z", "Z", 1),
		"signal": kind, "target_quantity": quantity,
	}
	return hashPaperProposalForTest(t, body)
}

func hashPaperProposalForTest(t *testing.T, body map[string]any) []byte {
	t.Helper()
	delete(body, "proposal_sha256")
	raw, err := strategyCanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	body["proposal_sha256"] = hex.EncodeToString(hash[:])
	raw, err = strategyCanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPaperProposalPythonProducerToGoAdmission(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	research, err := os.ReadFile(filepath.Join(root, "contracts/fixtures/strategy-market-bars.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "research.csv"), []byte(strings.ReplaceAll(string(research), ",SMA,", ",005930,")), 0600); err != nil {
		t.Fatal(err)
	}
	csv := "bar_at,symbol,venue,timezone,interval,open,high,low,close,volume,open_at,source_available_at,fetched_at\n"
	for i, price := range []string{"3", "2", "1", "4"} {
		day := fmt.Sprintf("2026-05-%02d", i+1)
		csv += fmt.Sprintf("%sT06:30:00Z,005930,KRX,Asia/Seoul,1d,%s,%s,%s,%s,100,%sT00:00:00Z,%sT06:31:00Z,%sT06:32:00Z\n", day, price, price, price, price, day, day, day)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.csv"), []byte(csv), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-c", `import json,sys
from pathlib import Path
from omni_research.improve import run_experiment
from omni_research.signal_cli import generate_proposal
root, directory = map(Path,sys.argv[1:])
config=json.loads((root/'contracts/fixtures/strategy-improvement-config.json').read_text())
config['strategy']['fast_windows']=[2]
config['strategy']['slow_windows']=[3]
artifact=run_experiment(directory/'research.csv',config)
proposal=generate_proposal(directory/'latest.csv',directory/'research.csv',artifact)
print(json.dumps({'artifact':artifact,'proposal':proposal},ensure_ascii=False))`, root, dir)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(root, "services/research"))
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python producer: %v %s", err, raw)
	}
	var produced struct {
		Artifact json.RawMessage `json:"artifact"`
		Proposal json.RawMessage `json:"proposal"`
	}
	if err := json.Unmarshal(raw, &produced); err != nil {
		t.Fatal(err)
	}
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-05-01T07:00:00Z") }
	ctx := context.Background()
	evidence, err := svc.registerStrategyEvidence(ctx, produced.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := svc.selectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.openPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selection.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return mustTime("2026-05-04T07:00:00Z") }
	result, err := svc.executeLocalPaper(ctx, k2aAccountRef, selection.CurrentEventID, produced.Proposal, []byte(csv), []byte(strings.ReplaceAll(string(research), ",SMA,", ",005930,")))
	if err != nil || result == nil || result.Order == nil || result.Order.Status != "OPEN" || result.Order.Quantity != "10" {
		t.Fatalf("cross-language local execution: %+v %v", result, err)
	}
}
