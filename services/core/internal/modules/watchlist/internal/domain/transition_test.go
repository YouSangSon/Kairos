package domain

import (
	"errors"
	"testing"
	"time"
)

func TestTransitionPreservesNilAndEmptyEntries(t *testing.T) {
	for _, tc := range []struct {
		name              string
		entries           []WatchlistEntry
		expected          int64
		changed, conflict bool
	}{
		{"nil matching revision", nil, 1, true, false},
		{"nil stale revision", nil, 0, false, true},
		{"empty matching revision", []WatchlistEntry{}, 1, false, false},
		{"empty retry", []WatchlistEntry{}, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &WatchlistState{Revision: 1, Entries: tc.entries}
			clocks := 0
			next, changed, err := Transition(state, WatchlistChange{ExpectedRevision: tc.expected, Entries: []WatchlistEntry{}}, func() time.Time { clocks++; return mustTime("2026-01-02T00:00:00Z") })
			if changed != tc.changed || errors.Is(err, ErrChanged) != tc.conflict || (err != nil && !tc.conflict) {
				t.Fatalf("next=%+v changed=%v err=%v", next, changed, err)
			}
			if tc.changed {
				if next.Revision != 2 || next.Entries == nil || len(next.Entries) != 0 || clocks != 1 {
					t.Fatalf("empty transition changed: %+v clocks=%d", next, clocks)
				}
			} else if clocks != 0 || (!tc.conflict && next != state) {
				t.Fatalf("retry/no-op changed state or clock: %+v clocks=%d", next, clocks)
			}
		})
	}
}
