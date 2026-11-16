package domain

import "testing"

func TestWatchlistStateAndChangeInvariants(t *testing.T) {
	if !ValidRevision(0) || !ValidRevision(watchlistRevisionLimit) || ValidRevision(-1) || ValidRevision(watchlistRevisionLimit+1) {
		t.Fatal("revision bounds changed")
	}
	entry := WatchlistEntry{Venue: "XNAS", Symbol: "AAPL", Currency: "USD"}
	for _, change := range []WatchlistChange{
		{ExpectedRevision: -1, Entries: []WatchlistEntry{}},
		{ExpectedRevision: watchlistRevisionLimit + 1, Entries: []WatchlistEntry{}},
		{ExpectedRevision: 0, Entries: nil},
		{ExpectedRevision: 0, Entries: []WatchlistEntry{entry, entry}},
	} {
		if _, err := ValidateChange(change); err != ErrInput {
			t.Fatalf("invalid change accepted: %+v, %v", change, err)
		}
	}
	if entries, err := ValidateChange(WatchlistChange{Entries: []WatchlistEntry{entry}}); err != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatalf("valid change rejected: %+v %v", entries, err)
	}
	at := "2026-01-01T00:00:00Z"
	bad := "not-a-clock"
	for _, tc := range []struct {
		name  string
		state WatchlistState
		valid bool
	}{
		{"initial empty", WatchlistState{Revision: 0, Entries: []WatchlistEntry{}}, true},
		{"initial must be empty", WatchlistState{Revision: 0, Entries: []WatchlistEntry{entry}}, false},
		{"initial has no clock", WatchlistState{Revision: 0, Entries: []WatchlistEntry{}, UpdatedAt: &at}, false},
		{"saved needs clock", WatchlistState{Revision: 1, Entries: []WatchlistEntry{entry}}, false},
		{"saved canonical clock", WatchlistState{Revision: 1, Entries: []WatchlistEntry{entry}, UpdatedAt: &at}, true},
		{"saved invalid clock", WatchlistState{Revision: 1, Entries: []WatchlistEntry{entry}, UpdatedAt: &bad}, false},
		{"nil entries", WatchlistState{Revision: 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StateMetadataValid(tc.state); got != tc.valid {
				t.Fatalf("metadata validity=%v; want %v", got, tc.valid)
			}
		})
	}
}
