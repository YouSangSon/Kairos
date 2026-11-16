package sqlite

import (
	"context"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	"path/filepath"
	"testing"
)

func TestG38F2BackupV15PreservesActiveLeaseButRejectsActivation(t *testing.T) {
	ctx := context.Background()
	svc, _ := g38EPerformanceWindow(t, []string{"100"})
	claim, err := svc.acquirePaperRunnerLease(ctx, k2aAccountRef)
	if err != nil {
		t.Fatal(err)
	}
	source, err := provePaperRunnerLeaseRecovery(ctx, svc.db)
	if err != nil || source.Leases != 1 || source.Active != 1 {
		t.Fatalf("active source runner lease proof=%+v err=%v", source, err)
	}

	backup := filepath.Join(t.TempDir(), "g38f2-active-v15.db")
	golden := mixedWriteCurrentSnapshot(t, svc)
	manifest, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: backup + ".manifest.json"})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.PaperRunnerLeaseStateSHA256 != source.SHA256 || manifest.PaperRunnerLeaseCount != 1 ||
		manifest.ActivePaperRunnerLeaseCount != 1 || manifest.VerificationReceipt.CandidatePaperRunnerLeaseStateSHA256 != source.SHA256 ||
		manifest.VerificationReceipt.EligibleForActivation {
		t.Fatalf("active runner lease was not preserved and activation-blocked: %+v", manifest)
	}
	candidate, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	candidateProof, proofErr := provePaperRunnerLeaseRecovery(ctx, &database{candidate})
	fresh := newService(candidate, svc.now, func(prefix string) string { return "fresh_" + prefix })
	_, freshClaimErr := fresh.acquirePaperRunnerLease(ctx, k2aAccountRef)
	_, freshHeartbeatErr := fresh.heartbeatPaperRunnerLease(ctx, claim)
	freshReleaseErr := fresh.releasePaperRunnerLease(ctx, claim)
	closeErr := candidate.Close()
	if proofErr != nil || freshClaimErr == nil || freshHeartbeatErr == nil || freshReleaseErr == nil || closeErr != nil || candidateProof != source {
		t.Fatalf("active runner lease changed or captured owner was reusable: source=%+v candidate=%+v errors=(%v,%v,%v,%v,%v)", source, candidateProof, proofErr, freshClaimErr, freshHeartbeatErr, freshReleaseErr, closeErr)
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(context.Background(), backup, golden, backup+".manifest.json"); err == nil {
		t.Fatal("active runner lease backup was eligible for activation")
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyRestore(context.Background(), backup, golden); err == nil {
		t.Fatal("active runner lease candidate passed restore activation")
	}
}
