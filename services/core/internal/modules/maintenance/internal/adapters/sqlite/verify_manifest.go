package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/application"
	pool "omni-folio/services/core/internal/storage/sqlite"
	"os"
	"path/filepath"
)

func (s *Store) VerifyManifest(ctx context.Context, path, goldenPath, manifestPath string) (resultErr error) {
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest contracts.BackupManifest
	dec := json.NewDecoder(bytes.NewReader(manifestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return fmt.Errorf("backup manifest: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(manifestBytes, &fields); err != nil {
		return fmt.Errorf("backup manifest fields: %w", err)
	}
	var receiptFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["verification_receipt"], &receiptFields); err != nil {
		return fmt.Errorf("backup verification receipt fields: %w", err)
	}
	policy, err := application.ValidateManifest(manifest, fields, receiptFields)
	if err != nil {
		return err
	}
	receipt := manifest.VerificationReceipt
	dbSHA, size, err := hashFile(path)
	if err != nil {
		return err
	}
	snapshotSHA, _, err := hashFile(goldenPath)
	if err != nil {
		return err
	}
	if manifest.DBSHA256 != dbSHA || receipt.CandidateDBSHA256 != dbSHA || manifest.SizeBytes != size {
		return errors.New("backup database hash or size mismatch")
	}
	if manifest.ExpectedSnapshotSHA256 != snapshotSHA || receipt.CandidateSnapshotSHA256 != snapshotSHA {
		return errors.New("backup snapshot hash mismatch")
	}
	sourceDB, err := pool.OpenReadOnlyExisting(ctx, path)
	if err != nil {
		return err
	}
	var firstSchema, sourceSchema, schemaCount int
	if err := sourceDB.QueryRow(`SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`).Scan(&firstSchema, &sourceSchema, &schemaCount); err != nil {
		_ = sourceDB.Close()
		return err
	}
	declaredSchema := fmt.Sprintf("omni-folio.sqlite.v%d", sourceSchema)
	if firstSchema != 1 || schemaCount != sourceSchema || manifest.SchemaVersion != declaredSchema {
		_ = sourceDB.Close()
		return errors.New("backup manifest schema does not match candidate")
	}
	sourceFacts, err := s.readProofs(ctx, sourceDB, sourceSchema, !policy.NonArchivalManifest, false)
	if err != nil {
		_ = sourceDB.Close()
		return err
	}
	if err := application.VerifySourceManifest(manifest, policy, sourceFacts); err != nil {
		_ = sourceDB.Close()
		return err
	}
	if !policy.HasOwnerFXManifest {

		var present int
		if err := sourceDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='owner_fx_series_versions'`).Scan(&present); err != nil || present != 0 {
			_ = sourceDB.Close()
			return errors.New("legacy backup cannot contain an owner FX journal")
		}
	}
	if !policy.HasOwnerPriceManifest {

		var present int
		if err := sourceDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='owner_price_series_versions'`).Scan(&present); err != nil || present != 0 {
			_ = sourceDB.Close()
			return errors.New("legacy backup cannot contain an owner price journal")
		}
	}
	if !policy.HasWatchlistManifest {

		var present int
		if err := sourceDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='watchlist_state'`).Scan(&present); err != nil || present != 0 {
			_ = sourceDB.Close()
			return errors.New("legacy backup cannot contain a watchlist state")
		}
	}
	if !policy.CurrentV21Manifest {

		var present int
		if err := sourceDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('paper_promotion_claims','paper_promotion_decisions')`).Scan(&present); err != nil || present != 0 {
			_ = sourceDB.Close()
			return errors.New("legacy backup cannot contain paper promotion journals")
		}
	}
	if err := sourceDB.Close(); err != nil {
		return err
	}

	verificationPath := path
	temporaryDir := ""
	defer func() {
		if temporaryDir == "" {
			return
		}
		if err := os.RemoveAll(temporaryDir); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove migrated restore candidate: %w", err))
		}
	}()
	if manifest.SchemaVersion != backupSchema {
		temporaryDir, err = os.MkdirTemp("", "omni-folio-restore-legacy.*")
		if err != nil {
			return err
		}
		verificationPath = filepath.Join(temporaryDir, "candidate.db")
		if err := copyFile(path, verificationPath); err != nil {
			return err
		}
		candidateDB, err := pool.OpenExisting(verificationPath)
		if err != nil {
			return err
		}
		if err := s.migrateCandidate(ctx, candidateDB); err != nil {
			_ = candidateDB.Close()
			return fmt.Errorf("migrate restored legacy candidate: %w", err)
		}
		if err := candidateDB.Close(); err != nil {
			return err
		}
	}

	candidateFacts, err := s.verifyCandidate(ctx, verificationPath, goldenPath, !policy.NonArchivalManifest)
	if err != nil {
		return err
	}
	return application.VerifyCandidateManifest(manifest, policy, candidateFacts)
}
