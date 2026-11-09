package ownership

import "testing"

func TestUnknownNamesCannotInheritAnOwner(t *testing.T) {
	for _, name := range []string{"events_secret", "events_new", "owner_price_series_versions_new", "watchlist_state_new", "paper_runner_leases_unknown_trigger"} {
		if _, ok := Owner(name); ok {
			t.Errorf("unknown table %q inherited ownership", name)
		}
		if _, ok := ObjectOwner(name); ok {
			t.Errorf("unknown object %q inherited ownership", name)
		}
	}
	for _, name := range []string{"events_no_update", "execution_authority_latest_idx", "owner_price_series_latest_idx", "strategy_selection_events_new"} {
		if _, ok := ObjectOwner(name); !ok {
			t.Errorf("actual schema object %q lost ownership", name)
		}
	}
}
