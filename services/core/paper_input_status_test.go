package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func paperInputStatusFixture(t *testing.T) (*Service, string, string, []byte, []byte, []byte) {
	t.Helper()
	svc, path, research, evidence, selected, rows := localPaperRestartFixture(t)
	bars := paperSnapshotCSV(rows...)
	proposal := localPaperRestartProposal(t, svc, evidence.ResultSHA256, rows[len(rows)-1], bars, "golden_cross")
	return svc, path, selected.CurrentEventID, proposal, bars, research
}

func paperInputStatusBundle(t *testing.T, proposal, bars, research []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"schema_version": "paper-input-bundle.v1", "mode": "paper_bundle_only",
		"proposal": json.RawMessage(proposal), "bars_csv": string(bars), "research_csv": string(research),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func inspectPaperInputForTest(t *testing.T, svc *Service, selection string, raw []byte) *PaperInputStatus {
	t.Helper()
	var sequence int
	var name, path string
	if err := svc.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, err := openReadOnlyExistingDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := newService(db, svc.now, randomID).paperInputStatus(context.Background(), k2aAccountRef, selection, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completion != "unknown" || result.ExecutionAuthorized || result.Freshness != "unverified" {
		t.Fatalf("inspection granted authority or completion: %+v", result)
	}
	return result
}

func TestPaperInputStatusTracksAnEarlierVerifiedBackup(t *testing.T) {
	svc, _, selection, proposal, bars, research := paperInputStatusFixture(t)
	backup := filepath.Join(t.TempDir(), "before-input.db")
	golden := writeCurrentSnapshot(t, svc.db)
	if _, err := createBackup(svc.db, backup, golden, backup+".manifest.json", svc.now, svc.id); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(backup, golden); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.executeLocalPaper(context.Background(), k2aAccountRef, selection, proposal, bars, research); err != nil {
		t.Fatal(err)
	}
	raw := paperInputStatusBundle(t, proposal, bars, research)
	if result := inspectPaperInputForTest(t, svc, selection, raw); !result.SignalRecorded {
		t.Fatal("current database did not observe admission")
	}
	db, err := openReadOnlyExistingDB(context.Background(), backup)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	older := newService(db, svc.now, randomID)
	result := inspectPaperInputForTest(t, older, selection, raw)
	if result.BarState != "not_observed_in_this_database" || result.SignalRecorded || result.OrderStatus != nil {
		t.Fatalf("inspection reused facts absent from the restored database: %+v", result)
	}
}
