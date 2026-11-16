package domain

import (
	"errors"
	"testing"
	"time"
)

func TestOwnerRecordTimePolicy(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name, previous, fetched, want string
		rejected                      bool
	}{
		{"equal", "2026-01-02T03:04:05Z", "2026-01-02T03:04:05Z", "", false},
		{"future capture", "", "2026-01-02T03:04:06Z", "", true},
		{"backwards", "2026-01-02T03:04:06Z", "invalid", "owner data record clock moved backwards", false},
		{"invalid capture", "", "invalid", "owner data capture time is invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := OwnerRecordedAt(tc.previous, tc.fetched, now)
			if tc.rejected {
				if !errors.Is(err, ErrCaptureFuture) {
					t.Fatalf("future capture error = %v", err)
				}
				return
			}
			if tc.want != "" {
				if err == nil || err.Error() != tc.want {
					t.Fatalf("error = %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil || got != "2026-01-02T03:04:05Z" {
				t.Fatalf("record = %q, %v", got, err)
			}
		})
	}
}

func TestVersionAndListingPolicies(t *testing.T) {
	if VersionConflicts("data", "data") || !VersionConflicts("old", "new") {
		t.Fatal("version identity policy changed")
	}
	if ListingDecision("DECLARE", "instrument", "instrument", true) != ListingDuplicate {
		t.Fatal("same declaration must be a retry")
	}
	if ListingDecision("DECLARE", "other", "instrument", true) != ListingConflict {
		t.Fatal("different declaration must conflict")
	}
	if ListingDecision("REVOKE", "other", "instrument", true) != ListingMissing {
		t.Fatal("revoke must name current instrument")
	}
	if ListingTransitionValid(ListingEvent{EventType: "REVOKE", ExpectedPreviousEventID: "no_event"}, ListingEvent{}, false) {
		t.Fatal("initial revoke admitted")
	}
}

func TestObservationAndPointPolicies(t *testing.T) {
	input := PriceInput{Source: "kiwoom_mock", InstrumentID: "instrument", FetchedAt: "new"}
	stored := PriceInput{Source: "kiwoom_mock", InstrumentID: "instrument", FetchedAt: "old"}
	if !SamePriceObservation(stored, input) {
		t.Fatal("Kiwoom recollection must preserve source identity")
	}
	input.Source, stored.Source = "local_fixture", "local_fixture"
	if SamePriceObservation(stored, input) {
		t.Fatal("fixture capture time must bind identity")
	}
	fetched := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !SeriesPointAdmitted("0", "2026-01-01T00:00:00Z", fetched, time.Time{}, 0, false) {
		t.Fatal("benchmark zero level must remain valid")
	}
	if SeriesPointAdmitted("0", "2026-01-01T00:00:00Z", fetched, time.Time{}, 0, true) {
		t.Fatal("owner zero value must remain invalid")
	}
	if SeriesPointAdmitted("1", "2026-01-01T00:00:00Z", fetched, fetched, 1, true) {
		t.Fatal("duplicate point time admitted")
	}
	if SeriesPointCountAllowed(10000) {
		t.Fatal("point cap admitted an extra point")
	}
}
