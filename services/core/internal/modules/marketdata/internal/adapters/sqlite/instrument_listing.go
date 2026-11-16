package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
)

const (
	instrumentListingDeclare   = "DECLARE"
	instrumentListingRevoke    = "REVOKE"
	instrumentListingAuthority = "owner_declared"
	noInstrumentListingEvent   = "no_event"
)

var errInstrumentListingNotFound = c.ErrListingNotFound

type instrumentListingRecoveryProof struct {
	SHA256 string
	Events int
	Active int
}

// ponytail: replay the small owner-managed registry on every authority check; add a projection only after measured volume makes it hot.
func replayInstrumentListingRegistry(ctx context.Context, q orderQuerier) (map[string]InstrumentListingEvent, instrumentListingRecoveryProof, error) {
	var storedSHA string

	rows, err := q.QueryContext(ctx, `SELECT sequence,event_id,event_type,authority,instrument_id,venue,symbol,currency,expected_previous_event_id,record_sha256,recorded_at
		FROM instrument_listing_events ORDER BY sequence`)
	if err != nil {
		return nil, instrumentListingRecoveryProof{}, err
	}
	defer rows.Close()
	active := map[string]InstrumentListingEvent{}
	latest := map[string]InstrumentListingEvent{}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	events := 0
	for rows.Next() {
		var sequence int64
		var event InstrumentListingEvent
		if err := rows.Scan(&sequence, &event.EventID, &event.EventType, &event.Authority, &event.InstrumentID,
			&event.Venue, &event.Symbol, &event.Currency, &event.ExpectedPreviousEventID, &storedSHA,
			&event.RecordedAt); err != nil {
			return nil, instrumentListingRecoveryProof{}, err
		}
		if err := validateInstrumentListingEvent(event); err != nil {
			return nil, instrumentListingRecoveryProof{}, fmt.Errorf("instrument listing event %q metadata mismatch: %w", event.EventID, err)
		}
		wantSHA, err := instrumentListingEventSHA(event)
		if err != nil || wantSHA != storedSHA {
			return nil, instrumentListingRecoveryProof{}, fmt.Errorf("instrument listing event %q metadata or hash mismatch", event.EventID)
		}
		key := instrumentListingKey(event.Venue, event.Symbol, event.Currency)
		previous, exists := latest[key]
		if !domain.ListingTransitionValid(domain.ListingEvent(event), domain.ListingEvent(previous), exists) {
			if !exists {
				return nil, instrumentListingRecoveryProof{}, fmt.Errorf("instrument listing event %q has an invalid initial transition", event.EventID)
			}
			return nil, instrumentListingRecoveryProof{}, fmt.Errorf("instrument listing event %q has an invalid transition", event.EventID)
		}
		if err := encoder.Encode([]any{sequence, event.EventID, event.EventType, event.Authority, event.InstrumentID,
			event.Venue, event.Symbol, event.Currency, event.ExpectedPreviousEventID, storedSHA, event.RecordedAt}); err != nil {
			return nil, instrumentListingRecoveryProof{}, err
		}
		latest[key] = event
		if event.EventType == instrumentListingDeclare {
			active[key] = event
		} else {
			delete(active, key)
		}
		events++
	}
	if err := rows.Err(); err != nil {
		return nil, instrumentListingRecoveryProof{}, err
	}
	return active, instrumentListingRecoveryProof{
		SHA256: hex.EncodeToString(hash.Sum(nil)), Events: events, Active: len(active),
	}, nil
}

func proveInstrumentListingRecovery(ctx context.Context, q orderQuerier) (instrumentListingRecoveryProof, error) {
	_, proof, err := replayInstrumentListingRegistry(ctx, q)
	return proof, err
}

func resolveInstrumentListing(ctx context.Context, q orderQuerier, venue, symbol, currency string) (*InstrumentListingEvent, error) {
	if !domain.ListingQueryValid(venue, symbol, currency) {
		return nil, errors.New("instrument listing query is invalid")
	}
	active, _, err := replayInstrumentListingRegistry(ctx, q)
	if err != nil {
		return nil, err
	}
	event, ok := active[instrumentListingKey(venue, symbol, currency)]
	if !ok {
		return nil, errInstrumentListingNotFound
	}
	return &event, nil
}
