package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestG38BRegistryRejectsUnsafeExecutionContract(t *testing.T) {
	valid := strategyArtifact(t, nil)
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing field", func(v map[string]any) { delete(v, "fee") }},
		{"extra field", func(v map[string]any) { v["commission_currency"] = "KRW" }},
		{"zero starting cash", func(v map[string]any) { v["starting_cash"] = "0" }},
		{"negative fee", func(v map[string]any) { v["fee"] = "-1" }},
		{"negative tax", func(v map[string]any) { v["tax"] = "-0.001" }},
		{"excess tax", func(v map[string]any) { v["tax"] = "1.0001" }},
		{"negative slippage", func(v map[string]any) { v["slippage_bps"] = "-1" }},
		{"excess slippage", func(v map[string]any) { v["slippage_bps"] = "10000" }},
		{"zero delay", func(v map[string]any) { v["delay_bars"] = "0" }},
		{"fractional delay", func(v map[string]any) { v["delay_bars"] = "1.5" }},
		{"zero participation", func(v map[string]any) { v["max_participation"] = "0" }},
		{"excess participation", func(v map[string]any) { v["max_participation"] = "1.1" }},
		{"noncanonical money", func(v map[string]any) { v["starting_cash"] = "01" }},
		{"negative zero fee", func(v map[string]any) { v["fee"] = "-0" }},
		{"numeric money", func(v map[string]any) { v["starting_cash"] = json.Number("10000") }},
		{"wrong signal price", func(v map[string]any) { v["signal_price"] = "same_bar_close" }},
		{"wrong fill price", func(v map[string]any) { v["fill_price"] = "same_bar_close" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc, _ := testService(t, nil, nil)
			artifact := rehashedStrategyArtifact(t, valid, func(result map[string]any) {
				test.mutate(result["execution"].(map[string]any))
			})
			if _, err := svc.registerStrategyEvidence(context.Background(), artifact); err == nil {
				t.Fatal("unsafe execution contract was admitted")
			}
			var count int
			if err := svc.db.QueryRow(`SELECT COUNT(*) FROM strategy_research_evidence`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected execution contract left evidence: count=%d err=%v", count, err)
			}
		})
	}
}

func TestG38C1LoadsOnlyCurrentSelectedExecutionPolicy(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	ctx := context.Background()
	evidence, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.SelectPaperCandidate(ctx, evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := loadCurrentStrategyExecutionPolicy(ctx, svc.db, evidence.ResultSHA256, selected.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.StartingCash != "10000" || policy.Fee != "1" || policy.Tax != "0.001" || policy.SlippageBPS != "10" ||
		policy.DelayBars != 1 || policy.MaxParticipation != "0.5" || policy.SignalPrice != "bar_close" ||
		policy.FillPrice != "next_eligible_bar_open" || policy.SHA256 != "84dab9f69764b1d9c880d45b2e1b440db5b6f0b9cb48ae7c3850ad831d382b23" {
		t.Fatalf("execution policy=%+v", policy)
	}
	if _, err := svc.RollbackPaperCandidate(ctx, selected.CurrentEventID, selected.CurrentEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCurrentStrategyExecutionPolicy(ctx, svc.db, evidence.ResultSHA256, selected.CurrentEventID); err == nil {
		t.Fatal("superseded selection loaded an execution policy")
	}
}

func rehashedStrategyArtifact(t testing.TB, artifact []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(artifact))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	mutate(result)
	delete(result, "result_sha256")
	body, err := strategyCanonicalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	result["result_sha256"] = hex.EncodeToString(hash[:])
	canonical, err := strategyCanonicalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestG3RegistryRollsBackOnlyToThePreviousSelection(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	ctx := context.Background()
	first, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.registerStrategyEvidence(ctx, strategyArtifact(t, func(config map[string]any) {
		config["experiment_id"] = "sma-grid-fixture-002"
		config["strategy"].(map[string]any)["version"] = "1.0.1"
	}))
	if err != nil {
		t.Fatal(err)
	}
	firstState, err := svc.SelectPaperCandidate(ctx, first.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	secondState, err := svc.SelectPaperCandidate(ctx, second.ResultSHA256, firstState.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RollbackPaperCandidate(ctx, secondState.CurrentEventID, firstState.CurrentEventID); err == nil {
		t.Fatal("rollback accepted a source event other than the current selection event")
	}
	rolledBack, err := svc.RollbackPaperCandidate(ctx, secondState.CurrentEventID, secondState.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.SelectedResultSHA256 != first.ResultSHA256 {
		t.Fatalf("rollback selected %q, want previous %q", rolledBack.SelectedResultSHA256, first.ResultSHA256)
	}
	noStrategy, err := svc.RollbackPaperCandidate(ctx, rolledBack.CurrentEventID, rolledBack.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	if noStrategy.SelectedResultSHA256 != noStrategySelection {
		t.Fatalf("second rollback selected %q, want %q", noStrategy.SelectedResultSHA256, noStrategySelection)
	}

	for _, statement := range []string{
		`UPDATE strategy_research_evidence SET target='no_promotion'`,
		`DELETE FROM strategy_research_evidence`,
		`UPDATE strategy_selection_events SET reason_code='manual_selection'`,
		`DELETE FROM strategy_selection_events`,
	} {
		if _, err := svc.db.Exec(statement); err == nil {
			t.Fatalf("insert-only registry accepted mutation: %s", statement)
		}
	}
	proof, err := proveStrategyRegistryRecovery(ctx, svc.db)
	if err != nil || proof.Evidence != 2 || proof.Events != 4 || proof.SelectedResultSHA256 != noStrategySelection {
		t.Fatalf("registry recovery proof=%+v err=%v", proof, err)
	}
}

func TestG3RegistryRecoveryRejectsTamperedEvidence(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	evidence, err := svc.registerStrategyEvidence(context.Background(), strategyArtifact(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SelectPaperCandidate(context.Background(), evidence.ResultSHA256, noStrategySelectionEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER strategy_research_evidence_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE strategy_research_evidence SET artifact_sha256=?`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := proveStrategyRegistryRecovery(context.Background(), svc.db); err == nil {
		t.Fatal("tampered strategy evidence was certified")
	}
}

func strategyArtifact(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(filepath.Dir(fixturePath("strategy-improvement-config.json")), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", "strategy-improvement-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(configBytes, &config); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(config)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	configBytes, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-m", "omni_research.improve_cli",
		"--bars", filepath.Join(root, "contracts", "fixtures", "strategy-market-bars.csv"),
		"--config", configPath,
	)
	command.Env = append(os.Environ(),
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONPATH="+filepath.Join(root, "services", "research"),
	)
	artifact, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generate Python strategy artifact: %v\n%s", err, artifact)
	}
	return artifact
}
