package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/application"
	pool "omni-folio/services/core/internal/storage/sqlite"
	"os"
)

func (s *Store) verifyCandidate(ctx context.Context, path, goldenPath string, archival bool) (application.BackupFacts, error) {
	db, err := pool.OpenReadOnlyExisting(ctx, path)
	if err != nil {
		return application.BackupFacts{}, err
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return application.BackupFacts{}, err
	}
	if integrity != "ok" {
		return application.BackupFacts{}, fmt.Errorf("integrity_check: %s", integrity)
	}
	if err := requireOrderRestoreSchema(db); err != nil {
		return application.BackupFacts{}, err
	}
	facts, err := s.readProofs(ctx, db, latestSchema, archival, true)
	if err != nil {
		return application.BackupFacts{}, err
	}
	goldenBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		return application.BackupFacts{}, err
	}
	var golden ledger.PortfolioSnapshot
	dec := json.NewDecoder(bytes.NewReader(goldenBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&golden); err != nil {
		return application.BackupFacts{}, fmt.Errorf("golden snapshot: %w", err)
	}
	actual := facts.Ledger.Snapshot
	if archival && golden.CostBasisPolicy == "" {
		golden.CostBasisPolicy = actual.CostBasisPolicy
	}
	if golden.CostBasisPolicy != actual.CostBasisPolicy {
		return application.BackupFacts{}, errors.New("golden snapshot replay policy does not match artifact version")
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		return application.BackupFacts{}, err
	}
	goldenJSON, err := json.Marshal(golden)
	if err != nil {
		return application.BackupFacts{}, err
	}
	if !bytes.Equal(actualJSON, goldenJSON) {
		return application.BackupFacts{}, errors.New("restored snapshot does not match golden")
	}
	return facts, nil
}
func (s *Store) VerifyRestore(ctx context.Context, path, goldenPath string) error {
	facts, err := s.verifyCandidate(ctx, path, goldenPath, false)
	if err != nil {
		return err
	}
	if facts.Trading.Runner.Active != 0 {
		return errors.New("restore candidate contains an active paper runner lease")
	}
	return nil
}
