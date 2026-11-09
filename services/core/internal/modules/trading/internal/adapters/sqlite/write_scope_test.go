package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"omni-folio/services/core/internal/modules/trading/internal/application"
)

func TestWriteScopePanicAndCancellationRollbackAndReleaseConnection(t *testing.T) {
	for _, kind := range []string{"panic", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			svc, _ := testService(t, nil, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			marker := errors.New("callback panic")
			func() {
				if kind == "panic" {
					defer func() {
						if recovered := recover(); recovered != marker {
							t.Fatalf("panic=%v", recovered)
						}
					}()
				}
				err := svc.Write(ctx, func(scope application.WriteScope) error {
					tx := scope.(*writeScope).tx
					if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES (28,'test')`); err != nil {
						return err
					}
					if kind == "panic" {
						panic(marker)
					}
					cancel()
					return nil
				})
				if kind == "cancel" && !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
					t.Fatalf("canceled write err=%v", err)
				}
			}()
			checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
			defer checkCancel()
			var count int
			if err := svc.db.QueryRowContext(checkCtx, `SELECT COUNT(*) FROM schema_migrations WHERE version=28`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("scope did not roll back and release connection: count=%d err=%v", count, err)
			}
			if err := svc.Write(checkCtx, func(application.WriteScope) error { return nil }); err != nil {
				t.Fatalf("next scope failed: %v", err)
			}
		})
	}
}

func TestPolicyFailedCommitPreservesCallerClaimAndAllJournals(t *testing.T) {
	svc, performance := g38EPerformanceWindow(t, []string{"100", "100"})
	claim, err := svc.acquirePaperRunnerLease(context.Background(), k2aAccountRef)
	if err != nil {
		t.Fatal(err)
	}
	beforeClaim := *claim
	beforeCounts := g38EJournalCounts(t, svc)
	beforeProof, err := provePaperRunnerLeaseRecovery(context.Background(), svc.db)
	if err != nil {
		t.Fatal(err)
	}
	// Deferred integrity fails at COMMIT, after the callback has renewed the claim.
	for _, statement := range []string{
		`CREATE TABLE test_commit_failure(version INTEGER REFERENCES schema_migrations(version) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER test_policy_commit_failure AFTER INSERT ON paper_performance_policy_events BEGIN INSERT INTO test_commit_failure VALUES (999); END`,
	} {
		if _, err := svc.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now := svc.now().Add(10 * time.Second)
	svc.now = func() time.Time { return now }
	event, err := svc.business().ApplyPaperPerformancePolicyTx(context.Background(), k2aAccountRef, performance.StrategySelectionEventID, performance.StrategyPerformanceID, claim)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") || event != nil {
		t.Fatalf("commit failure event=%+v err=%v", event, err)
	}
	if *claim != beforeClaim {
		t.Fatalf("failed commit mutated caller claim: before=%+v after=%+v", beforeClaim, *claim)
	}
	if after := g38EJournalCounts(t, svc); after != beforeCounts {
		t.Fatalf("failed commit changed journals: before=%+v after=%+v", beforeCounts, after)
	}
	afterProof, err := provePaperRunnerLeaseRecovery(context.Background(), svc.db)
	if err != nil || afterProof != beforeProof {
		t.Fatalf("failed commit changed lease proof: before=%+v after=%+v err=%v", beforeProof, afterProof, err)
	}
}
