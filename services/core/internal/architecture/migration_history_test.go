package architecture

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

//go:embed migration_history.json
var migrationHistory []byte

func TestRefactorPreservesHistoricalMigrations(t *testing.T) {
	var history map[string]string
	if err := json.Unmarshal(migrationHistory, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 27 {
		t.Fatalf("expected frozen migration history 001-027, got %d", len(history))
	}
	for path, expected := range history {
		source, err := os.ReadFile(filepath.Join("../../migrations", path))
		if err != nil {
			t.Error(err)
			continue
		}
		if actual := fmt.Sprintf("%x", sha256.Sum256(source)); actual != expected {
			t.Errorf("historical migration %s changed during behavior-preserving refactor", path)
		}
	}
}
