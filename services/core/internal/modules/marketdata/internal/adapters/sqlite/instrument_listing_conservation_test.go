package sqlite

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestInstrumentListingDeclareResolveRevokeAndCorrect(t *testing.T) {
	svc, _ := conservationService(t, []time.Time{
		priceMustTime("2026-08-30T03:00:00Z"),
		priceMustTime("2026-08-30T03:01:00Z"),
		priceMustTime("2026-08-30T03:02:00Z"),
	}, nil)
	samsung := InstrumentListingInput{
		InstrumentID: "instrument_005930", Venue: "XKRX", Symbol: "005930", Currency: "KRW",
	}

	declared, err := svc.DeclareListing(context.Background(), samsung)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.DeclareListing(context.Background(), samsung)
	if err != nil || !reflect.DeepEqual(declared, replayed) {
		t.Fatalf("idempotent declaration drifted: declared=%+v replayed=%+v err=%v", declared, replayed, err)
	}
	conflicting := samsung
	conflicting.InstrumentID = "instrument_other"
	if _, err := svc.DeclareListing(context.Background(), conflicting); err == nil {
		t.Fatal("active listing identity was rebound")
	}
	if _, err := svc.RevokeListing(context.Background(), samsung); err != nil {
		t.Fatal(err)
	}
	if listing, err := resolveInstrumentListing(context.Background(), svc.reader(), "XKRX", "005930", "KRW"); err == nil || listing != nil {
		t.Fatalf("revoked listing resolved: listing=%+v err=%v", listing, err)
	}
	corrected, err := svc.DeclareListing(context.Background(), conflicting)
	if err != nil || corrected.InstrumentID != "instrument_other" {
		t.Fatalf("corrected listing=%+v err=%v", corrected, err)
	}

	active, proof, err := replayInstrumentListingRegistry(context.Background(), svc.reader())
	if err != nil || len(active) != 1 || proof.Events != 3 || proof.Active != 1 {
		t.Fatalf("listing replay active=%+v proof=%+v err=%v", active, proof, err)
	}
}

func TestInstrumentListingRejectsStaleWrites(t *testing.T) {
	svc, _ := conservationService(t, nil, nil)
	listing := InstrumentListingInput{
		InstrumentID: "instrument_005930", Venue: "XKRX", Symbol: "005930", Currency: "KRW",
	}
	declared, err := svc.DeclareListing(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	stale := InstrumentListingEvent{
		EventID: "instrument_listing_stale", EventType: instrumentListingDeclare, Authority: instrumentListingAuthority,
		InstrumentID: listing.InstrumentID, Venue: listing.Venue, Symbol: listing.Symbol, Currency: listing.Currency,
		ExpectedPreviousEventID: noInstrumentListingEvent, RecordedAt: "2026-08-30T03:05:00Z",
	}
	recordSHA256, err := instrumentListingEventSHA(stale)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO instrument_listing_events(
		event_id,event_type,authority,instrument_id,venue,symbol,currency,expected_previous_event_id,record_sha256,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?)`, stale.EventID, stale.EventType, stale.Authority, stale.InstrumentID,
		stale.Venue, stale.Symbol, stale.Currency, stale.ExpectedPreviousEventID, recordSHA256, stale.RecordedAt); err == nil {
		t.Fatal("stale listing transition was stored")
	}
	if _, err := svc.db.Exec(`UPDATE instrument_listing_events SET instrument_id='instrument_other' WHERE event_id=?`, declared.EventID); err == nil {
		t.Fatal("listing event was updated")
	}
	if _, err := svc.db.Exec(`DELETE FROM instrument_listing_events WHERE event_id=?`, declared.EventID); err == nil {
		t.Fatal("listing event was deleted")
	}

}
