package main

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

func TestG3RegistryBackupRestoresSelectionProof(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	evidence, err := svc.registerStrategyEvidence(context.Background(), strategyArtifact(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.selectPaperCandidate(context.Background(), evidence.ResultSHA256, noStrategySelectionEvent)
	if err != nil {
		t.Fatal(err)
	}
	golden := writeCurrentSnapshot(t, svc.db)
	backup := filepath.Join(t.TempDir(), "backup.db")
	manifestPath := backup + ".manifest.json"
	manifest, err := createBackup(svc.db, backup, golden, manifestPath, svc.now, svc.id)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != backupFormat || manifest.SchemaVersion != backupSchema ||
		manifest.StrategyRegistrySHA256 == "" || manifest.StrategyEvidenceCount != 1 || manifest.StrategySelectionEventCount != 1 ||
		manifest.SelectedStrategyResultSHA256 != evidence.ResultSHA256 {
		t.Fatalf("backup omitted strategy registry proof: %+v", manifest)
	}
	if err := verifyManifest(backup, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	restored, err := proveStrategyRegistryRecovery(context.Background(), restoredDB)
	if err != nil || restored.CurrentEventID != selected.CurrentEventID || restored.SelectedResultSHA256 != evidence.ResultSHA256 {
		t.Fatalf("restored registry proof=%+v err=%v", restored, err)
	}

	tampered := readJSONMap(t, manifestPath)
	tampered["strategy_registry_sha256"] = strings.Repeat("0", 64)
	tamperedPath := filepath.Join(t.TempDir(), "tampered-manifest.json")
	writeJSONFile(t, tamperedPath, tampered)
	if err := verifyManifest(backup, golden, tamperedPath); err == nil {
		t.Fatal("manifest with a different strategy registry digest was accepted")
	}
	if _, err := restoredDB.Exec(`DROP TRIGGER strategy_selection_events_state_guard`); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(backup, golden); err == nil {
		t.Fatal("restore accepted a strategy registry without its state guard")
	}
}

func TestG3RegistryCLIImportsSelectsAndRollsBackLocalEvidence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "core.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(t.TempDir(), "strategy-result.json")
	if err := os.WriteFile(artifactPath, strategyArtifact(t, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"strategy-register", "-db", dbPath, "-artifact", artifactPath}); err != nil {
		t.Fatal(err)
	}
	var resultSHA string
	if err := db.QueryRow(`SELECT result_sha256 FROM strategy_research_evidence`).Scan(&resultSHA); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"strategy-select", "-db", dbPath, "-result-sha256", resultSHA, "-expected-current-event", noStrategySelectionEvent}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"strategy-status", "-db", dbPath}); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := db.QueryRow(`SELECT event_id FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"strategy-rollback", "-db", dbPath, "-expected-current-event", eventID, "-source-event", eventID}); err != nil {
		t.Fatal(err)
	}
	proof, err := proveStrategyRegistryRecovery(context.Background(), db)
	if err != nil || proof.SelectedResultSHA256 != noStrategySelection || proof.Events != 2 {
		t.Fatalf("CLI registry proof=%+v err=%v", proof, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func strategyArtifact(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
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
