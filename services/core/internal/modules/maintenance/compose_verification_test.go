package maintenance_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	maintenance "omni-folio/services/core/internal/modules/maintenance"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
)

func TestStandaloneVerifierExposesNoLiveCapabilityAndDefersOwners(t *testing.T) {
	calls := 0
	verifier, err := maintenance.ComposeVerification(maintenance.VerificationConfig{Now: time.Now, ID: func(s string) string { return s }, NewOwners: func(*sql.DB) (maintenance.Owners, error) {
		calls++
		t.Fatal("invalid evidence invoked owner factory")
		return maintenance.Owners{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := verifier.(c.Backups); ok {
		t.Fatal("verifier exposes live backup capability")
	}
	if _, ok := verifier.(c.Migrations); ok {
		t.Fatal("verifier exposes migration capability")
	}
	if _, ok := verifier.(c.Schema); ok {
		t.Fatal("verifier exposes live schema capability")
	}
	dir := t.TempDir()
	manifest := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(manifest, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, "candidate.db")
	if err := verifier.VerifyManifest(context.Background(), candidate, "missing-golden", manifest); err == nil || !strings.Contains(err.Error(), "backup manifest") {
		t.Fatalf("manifest did not fail before candidate: %v", err)
	}
	if calls != 0 {
		t.Fatal("constructor opened owners")
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatalf("invalid manifest created candidate: %v", err)
	}
}
