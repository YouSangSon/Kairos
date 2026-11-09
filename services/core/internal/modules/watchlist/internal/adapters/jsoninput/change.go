package jsoninput

import (
	"encoding/json"
	"omni-folio/services/core/internal/codec"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/modules/watchlist/internal/domain"
	"unicode/utf8"
)

func Decode(raw []byte) (*c.WatchlistChange, error) {
	if len(raw) == 0 || len(raw) > c.BodyLimit || !utf8.Valid(raw) || !codec.ValidJSONSurrogates(raw) {
		return nil, c.ErrInput
	}
	fields, err := codec.ClosedObject(raw, nil, "expected_revision", "entries")
	if err != nil {
		return nil, c.ErrInput
	}
	var change c.WatchlistChange
	var entries []json.RawMessage
	if json.Unmarshal(fields["expected_revision"], &change.ExpectedRevision) != nil || change.ExpectedRevision < 0 || change.ExpectedRevision > c.RevisionLimit ||
		json.Unmarshal(fields["entries"], &entries) != nil || entries == nil || len(entries) > 50 {
		return nil, c.ErrInput
	}
	change.Entries = []c.WatchlistEntry{}
	for _, rawEntry := range entries {
		if _, err := codec.ClosedObject(rawEntry, nil, "venue", "symbol", "currency"); err != nil {
			return nil, c.ErrInput
		}
		var entry c.WatchlistEntry
		if json.Unmarshal(rawEntry, &entry) != nil {
			return nil, c.ErrInput
		}
		change.Entries = append(change.Entries, entry)
	}
	change.Entries, err = domain.CanonicalEntries(change.Entries)
	return &change, err
}
