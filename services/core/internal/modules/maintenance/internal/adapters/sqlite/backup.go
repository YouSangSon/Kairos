package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/application"
	"os"
	"strings"
)

func (s *Store) CreateBackup(ctx context.Context, request contracts.BackupRequest) (_ *contracts.BackupManifest, resultErr error) {
	db, out, golden, manifestPath := s.db, request.OutputPath, request.GoldenPath, request.ManifestPath
	if _, err := os.Stat(out); err == nil {
		return nil, fmt.Errorf("backup target already exists: %s", out)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err := os.Stat(manifestPath); err == nil {
		return nil, fmt.Errorf("manifest target already exists: %s", manifestPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	candidateCreated, manifestCreated := false, false
	defer func() {
		if resultErr == nil {
			return
		}
		var cleanupErr error
		if manifestCreated {
			if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		if candidateCreated {
			if err := os.Remove(out); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		if cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("discard failed backup candidate: %w", cleanupErr))
		}
	}()
	source, err := s.readProofs(ctx, db, latestSchema, false, true)
	if err != nil {
		return nil, err
	}
	createdAt := s.now().UTC()
	quoted := strings.ReplaceAll(out, "'", "''")
	if _, err := db.Exec(`VACUUM INTO '` + quoted + `'`); err != nil {
		return nil, fmt.Errorf("consistent backup: %w", err)
	}
	candidateCreated = true
	candidate, err := s.verifyCandidate(ctx, out, golden, false)
	if err != nil {
		return nil, err
	}
	dbSHA, size, err := hashFile(out)
	if err != nil {
		return nil, err
	}
	snapshotSHA, _, err := hashFile(golden)
	if err != nil {
		return nil, err
	}
	manifest, err := application.BuildManifest(source, candidate, createdAt, s.now().UTC(), dbSHA, size, snapshotSHA, s.id)
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	manifestFile, err := os.OpenFile(manifestPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	manifestCreated = true
	if _, err := manifestFile.Write(encoded); err != nil {
		_ = manifestFile.Close()
		return nil, err
	}
	if err := manifestFile.Close(); err != nil {
		return nil, err
	}
	return manifest, nil
}
