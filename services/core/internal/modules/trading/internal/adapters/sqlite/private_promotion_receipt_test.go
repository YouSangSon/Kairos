package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/trading/construction"
	"omni-folio/services/core/internal/modules/trading/contracts"
	tradingcli "omni-folio/services/core/internal/modules/trading/internal/adapters/cli"
	pool "omni-folio/services/core/internal/storage/sqlite"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPaperPromotionReceiptBackupAndSignalCutoff(t *testing.T) {
	f := newPaperPromotionFixture(t)
	ctx := context.Background()
	const attempt = "promotion-complete"
	receipt, err := f.svc.PromotePaperCandidate(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, f.bars)
	if err != nil || receipt == nil || receipt.Decision != "PROMOTE" || receipt.SelectionEventID == "" {
		t.Fatalf("promotion: %+v %v", receipt, err)
	}
	before, err := provePaperPromotionRecovery(ctx, f.svc.db)
	if err != nil || before.Claims != 1 || before.Decisions != 1 {
		t.Fatalf("promotion proof: %+v %v", before, err)
	}
	outputErr := errors.New("receipt output closed")
	statusArgs := []string{"-db", f.dbPath, "-attempt-id", attempt}
	if err := tradingcli.Run(ctx, "paper-promotion-status", statusArgs, privatePromotionCLIResources{f.svc.now, f.svc.id}, nil, privatePromotionFailWriter{outputErr}); !errors.Is(err, outputErr) {
		t.Fatalf("writer error lost: %v", err)
	}
	var out bytes.Buffer
	if err := tradingcli.Run(ctx, "paper-promotion-status", statusArgs, privatePromotionCLIResources{f.svc.now, f.svc.id}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var reported PaperPromotionReceipt
	if err := json.Unmarshal(out.Bytes(), &reported); err != nil || !reflect.DeepEqual(&reported, receipt) || bytes.Contains(out.Bytes(), []byte("input_csv")) || bytes.Contains(out.Bytes(), f.bars) {
		t.Fatalf("status did not preserve the public receipt: %s, %v", out.String(), err)
	}
	afterStatus, err := provePaperPromotionRecovery(ctx, f.svc.db)
	if err != nil || afterStatus != before {
		t.Fatalf("status retry changed journal: %+v %v", afterStatus, err)
	}
	golden := mixedWriteCurrentSnapshot(t, f.svc)
	backup := filepath.Join(t.TempDir(), "promotion.db")
	manifestPath := backup + ".manifest.json"
	manifest, err := mixedBackupModule(t, f.svc, f.svc.now, f.svc.id).CreateBackup(ctx, maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: manifestPath})
	if err != nil || manifest == nil || (manifest.PaperPromotionStateSHA256 != before.SHA256 || manifest.PaperPromotionClaimCount != before.Claims || manifest.PaperPromotionDecisionCount != before.Decisions || manifest.VerificationReceipt.CandidatePaperPromotionStateSHA256 != before.SHA256) {
		t.Fatalf("backup omitted promotion proof: %v", err)
	}
	if err := mixedBackupModule(t, f.svc, f.svc.now, f.svc.id).VerifyManifest(ctx, backup, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	restored, err := pool.OpenReadOnlyExisting(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredProof, err := provePaperPromotionRecovery(ctx, &database{restored})
	if err != nil || restoredProof != before {
		t.Fatalf("restored promotion proof changed: %+v %v", restoredProof, err)
	}
	end, err := time.Parse(time.RFC3339, receipt.ComparisonEnd)
	if err != nil {
		t.Fatal(err)
	}
	bars := checkPaperPromotionSignalAdmission(t, f, receipt.SelectionEventID, end, nil)
	rollback, err := f.svc.RollbackPaperCandidate(ctx, receipt.SelectionEventID, receipt.SelectionEventID)
	if err != nil || rollback == nil || rollback.SelectedResultSHA256 != f.incumbent.ResultSHA256 {
		t.Fatalf("rollback: %+v %v", rollback, err)
	}
	if err := validatePaperPromotionSignalCutoff(ctx, f.svc.db, f.incumbent.ResultSHA256, rollback.CurrentEventID, end.Format(time.RFC3339)); err != nil {
		t.Fatalf("incumbent inherited candidate cutoff: %v", err)
	}
	reselected, err := f.svc.SelectPaperCandidate(ctx, f.challenger.ResultSHA256, rollback.CurrentEventID)
	if err != nil {
		t.Fatal(err)
	}
	checkPaperPromotionSignalAdmission(t, f, reselected.CurrentEventID, end, bars)
	retry, err := f.svc.PromotePaperCandidate(ctx, k2aAccountRef, f.challenger.ResultSHA256, f.selection.CurrentEventID, attempt, f.bars)
	if err != nil || !reflect.DeepEqual(retry, receipt) {
		t.Fatalf("terminal retry after reselection changed receipt: %+v %v", retry, err)
	}
	afterRetry, err := provePaperPromotionRecovery(ctx, f.svc.db)
	if err != nil || afterRetry != before {
		t.Fatalf("terminal retry added journal entries: %+v %v", afterRetry, err)
	}
}

type privatePromotionCLIResources struct {
	now func() time.Time
	id  func(string) string
}

func (r privatePromotionCLIResources) OpenExisting(string) (construction.Session, error) {
	return nil, errors.New("promotion status fixture supports read-only access")
}
func (r privatePromotionCLIResources) OpenReadWrite(context.Context, string) (construction.Session, error) {
	return nil, errors.New("promotion status fixture supports read-only access")
}
func (r privatePromotionCLIResources) OpenReadOnly(ctx context.Context, path string) (construction.Session, error) {
	db, err := pool.OpenReadOnlyExisting(ctx, path)
	if err != nil {
		return nil, err
	}
	return privatePromotionCLISession{newService(db, r.now, r.id), ctx}, nil
}

type privatePromotionCLISession struct {
	store *Store
	ctx   context.Context
}

func (s privatePromotionCLISession) Trading() contracts.Trading { return s.store }
func (s privatePromotionCLISession) RequireSchema() error {
	return requireSchemaContext(s.ctx, s.store.db)
}
func (s privatePromotionCLISession) Close() error { return s.store.db.Close() }

type privatePromotionFailWriter struct{ err error }

func (w privatePromotionFailWriter) Write([]byte) (int, error) { return 0, w.err }
