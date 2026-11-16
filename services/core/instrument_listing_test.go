package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestInstrumentListingRejectsStaleWritesAndBindsBackupProof(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	listing := InstrumentListingInput{
		InstrumentID: "instrument_005930", Venue: "XKRX", Symbol: "005930", Currency: "KRW",
	}
	_, err := svc.declareInstrumentListing(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	golden := writeCurrentSnapshot(t, svc.db)
	backup := filepath.Join(t.TempDir(), "listing.db")
	manifestPath := backup + ".manifest.json"
	manifest, err := createBackup(svc.db, backup, golden, manifestPath, svc.now, svc.id)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.InstrumentListingEventCount != 1 || manifest.ActiveInstrumentListingCount != 1 ||
		len(manifest.InstrumentListingStateSHA256) != 64 || manifest.VerificationReceipt.InstrumentListingCheck != "ok" ||
		manifest.VerificationReceipt.CandidateInstrumentListingStateSHA256 != manifest.InstrumentListingStateSHA256 {
		t.Fatalf("backup omitted instrument listing proof: %+v", manifest)
	}
	if err := verifyManifest(backup, golden, manifestPath); err != nil {
		t.Fatal(err)
	}
	tampered := readJSONMap(t, manifestPath)
	tampered["active_instrument_listing_count"] = float64(2)
	tamperedPath := filepath.Join(t.TempDir(), "listing-tampered.manifest.json")
	writeJSONFile(t, tamperedPath, tampered)
	if err := verifyManifest(backup, golden, tamperedPath); err == nil {
		t.Fatal("backup with a mismatched instrument listing proof was accepted")
	}
	weakBackup := filepath.Join(t.TempDir(), "listing-missing-trigger.db")
	if _, err := createBackup(svc.db, weakBackup, golden, weakBackup+".manifest.json", svc.now, svc.id); err != nil {
		t.Fatal(err)
	}
	candidate, err := openExistingDB(weakBackup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`DROP TRIGGER instrument_listing_events_state_guard`); err != nil {
		_ = candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(weakBackup, golden); err == nil {
		t.Fatal("restore accepted instrument listings without the exact state guard")
	}
}
