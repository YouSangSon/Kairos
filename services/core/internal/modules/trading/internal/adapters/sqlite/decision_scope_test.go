package sqlite

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"strings"
	"testing"
	"time"
)

type observedDecisions struct {
	*Store
	commits []bool
}

func (j *observedDecisions) Decide(ctx context.Context, work func(application.WriteScope) (bool, error)) error {
	return j.Store.Decide(ctx, func(scope application.WriteScope) (bool, error) {
		commit, err := work(scope)
		j.commits = append(j.commits, commit)
		return commit, err
	})
}

func TestDecisionScopeKeepsRollbackAndCommitFailureDistinct(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	if _, err := svc.db.Exec(`CREATE TABLE decision_scope_probe(version INTEGER REFERENCES schema_migrations(version) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("reject proposed write")
	for _, test := range []struct {
		name      string
		commit    bool
		version   int
		workErr   error
		wantError bool
		wantCount int
	}{
		{"no change rolls back", false, 999, nil, false, 0},
		{"error rolls back", true, 27, rejected, true, 0},
		{"commit failure rolls back", true, 999, nil, true, 0},
		{"successful commit", true, 27, nil, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := svc.Decide(context.Background(), func(scope application.WriteScope) (bool, error) {
				_, err := scope.(*writeScope).tx.ExecContext(context.Background(), `INSERT INTO decision_scope_probe(version) VALUES(?)`, test.version)
				if err != nil {
					return false, err
				}
				return test.commit, test.workErr
			})
			if (err != nil) != test.wantError {
				t.Fatalf("decision error=%v", err)
			}
			if test.workErr != nil && !errors.Is(err, test.workErr) {
				t.Fatalf("lost work error: %v", err)
			}
			if test.name == "commit failure rolls back" && !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
				t.Fatalf("commit error=%v", err)
			}
			var count int
			if err := svc.db.QueryRow(`SELECT COUNT(*) FROM decision_scope_probe`).Scan(&count); err != nil || count != test.wantCount {
				t.Fatalf("durable rows=%d err=%v", count, err)
			}
		})
	}
}

func TestPerformanceCachedReadPreservesRollbackDisposition(t *testing.T) {
	svc, asOf := g38c3CashPerformanceFixture(t)
	observed := &observedDecisions{Store: svc}
	useCases := application.New(observed, svc, svc.now, svc.id, svc.executionOwner, svc.paperRunnerOwner)
	first, err := useCases.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf, nil)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := useCases.EvaluatePaperPerformance(context.Background(), k2aAccountRef, asOf, nil)
	if err != nil || *cached != *first {
		t.Fatalf("cached=%+v first=%+v err=%v", cached, first, err)
	}
	if len(observed.commits) != 2 || !observed.commits[0] || observed.commits[1] {
		t.Fatalf("new/cached commit dispositions=%v", observed.commits)
	}
}

func TestPerformanceCommitFailureKeepsClaimAndJournals(t *testing.T) {
	svc, asOf := g38c3CashPerformanceFixture(t)
	ctx := context.Background()
	claim, err := svc.acquirePaperRunnerLease(ctx, k2aAccountRef)
	if err != nil {
		t.Fatal(err)
	}
	beforeClaim := *claim
	beforeLease, err := loadPaperRunnerLease(ctx, svc.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE test_performance_commit_failure(version INTEGER REFERENCES schema_migrations(version) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER test_performance_commit_failure AFTER INSERT ON paper_performance_events BEGIN INSERT INTO test_performance_commit_failure VALUES (999); END`,
	} {
		if _, err := svc.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now := svc.now().Add(10 * time.Second)
	svc.now = func() time.Time { return now }
	event, err := svc.business().EvaluatePaperPerformance(ctx, k2aAccountRef, asOf, claim)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") || event != nil {
		t.Fatalf("failed commit event=%+v err=%v", event, err)
	}
	if *claim != beforeClaim {
		t.Fatalf("failed commit changed claim: before=%+v after=%+v", beforeClaim, *claim)
	}
	afterLease, err := loadPaperRunnerLease(ctx, svc.db)
	if err != nil || afterLease != beforeLease {
		t.Fatalf("failed commit changed lease: before=%+v after=%+v err=%v", beforeLease, afterLease, err)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM paper_performance_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed commit persisted performance count=%d err=%v", count, err)
	}
}
