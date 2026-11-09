package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"omni-folio/services/core/internal/modules/trading/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/internal/storage/ownership"
)

func TestRecoveryStatementsAreFiniteOwnedReads(t *testing.T) {
	statements, err := recoveryReadStatements()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if err := ownership.ValidateSQL("trading", statement.SQL, statement.Objects); err != nil {
			t.Errorf("%v: %s", err, statement.SQL)
		}
	}
}

func TestScopedBackupRecoveryRejectsUnavailableAndWrongPurposeReferences(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	registry := observation.New(svc.db.DB, svc.now)
	if err := svc.BindRecoveryReaders(registry); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	expected, err := svc.VerifyRecovery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	query := contracts.BackupQuery{Schema: 27}
	var saved storage.ObservationRef
	if err := registry.VerifyBackup(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		saved = ref
		if err := svc.VerifyStartup(ctx, ref); err != nil {
			return err
		}
		actual, err := svc.VerifyBackup(ctx, ref, query)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("scoped recovery changed proof: %+v != %+v", actual, expected)
		}
		if _, err := svc.MigrationFacts(ctx, ref, contracts.MigrationQuery{Schema: 26}); !errors.Is(err, observation.ErrReference) {
			t.Fatalf("backup reference entered migration reader: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []storage.ObservationRef{saved, storage.NewObservationRef(), {}} {
		if _, err := svc.VerifyBackup(ctx, ref, query); !errors.Is(err, observation.ErrReference) {
			t.Fatalf("unavailable reference accepted: %v", err)
		}
	}
	if err := registry.ReadValuation(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		if _, err := svc.VerifyBackup(ctx, ref, query); !errors.Is(err, observation.ErrReference) {
			t.Fatalf("valuation reference entered backup reader: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	other := observation.New(svc.db.DB, svc.now)
	if err := other.VerifyBackup(ctx, func(ref storage.ObservationRef, _ time.Time) error {
		if _, err := svc.VerifyBackup(ctx, ref, query); !errors.Is(err, observation.ErrReference) {
			t.Fatalf("foreign registry reference accepted: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
