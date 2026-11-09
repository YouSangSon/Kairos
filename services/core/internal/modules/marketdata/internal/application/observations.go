package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
	storage "omni-folio/services/core/internal/storage/contracts"
)

func (s *Service) ReadValuationInputs(ctx context.Context, ref storage.ObservationRef, query c.ValuationQuery) (c.ValuationInputs, error) {
	admitted, err := s.admitted(ctx, ref, query.Prices)
	if err != nil {
		return c.ValuationInputs{}, err
	}
	return s.store.ReadInputs(ctx, ref, admitted, query)
}

func (s *Service) VerifyBackup(ctx context.Context, ref storage.ObservationRef, query c.BackupQuery) (c.RecoveryProof, error) {
	if query.Schema == 0 {
		query.Schema = 27
	}
	admitted, err := s.admitted(ctx, ref, query.Schema >= 25)
	if err != nil {
		return c.RecoveryProof{}, err
	}
	return s.store.Verify(ctx, ref, admitted, query)
}

func (s *Service) VerifyStartup(ctx context.Context, ref storage.ObservationRef) error {
	if err := s.store.ProveBenchmark(ctx, ref); err != nil {
		return fmt.Errorf("benchmark series startup recovery: %w", err)
	}
	if err := s.store.ProveOwnerFX(ctx, ref); err != nil {
		return fmt.Errorf("owner FX series startup recovery: %w", err)
	}
	admitted, err := s.admitted(ctx, ref, true)
	if err != nil {
		return fmt.Errorf("owner price series startup recovery: %w", err)
	}
	if err := s.store.ProveOwnerPrice(ctx, ref, admitted); err != nil {
		return fmt.Errorf("owner price series startup recovery: %w", err)
	}
	return nil
}

func (s *Service) admitted(ctx context.Context, ref storage.ObservationRef, prices bool) (map[string]struct{}, error) {
	if !prices {
		return nil, nil
	}
	present, err := s.store.HasOwnerPrices(ctx, ref)
	if err != nil || !present {
		return nil, err
	}
	identities, err := s.ledger.AdmittedPriceIdentities(ctx, ref)
	return identities.Identities, err
}

func (s *Service) RecordFX(ctx context.Context, input c.FXObservationInput) (*c.FXObservation, error) {
	if err := s.decode.FX(input); err != nil {
		return nil, err
	}
	var result *c.FXObservation
	err := s.store.Write(ctx, func(scope Scope) error {
		stored, err := scope.FXBySource(ctx, input.Source, input.SourceObservationID)
		if err == nil {
			if stored.Source != input.Source || stored.SourceObservationID != input.SourceObservationID || stored.BaseCurrency != input.BaseCurrency || stored.QuoteCurrency != input.QuoteCurrency || stored.Rate != input.Rate || stored.ObservedAt != input.ObservedAt || stored.FetchedAt != input.FetchedAt {
				return errors.New("FX source observation identity is already bound to different data")
			}
			result = stored
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		value := c.FXObservation{ObservationID: s.id("fx_observation"), Source: input.Source, SourceObservationID: input.SourceObservationID, BaseCurrency: input.BaseCurrency, QuoteCurrency: input.QuoteCurrency, Rate: input.Rate, ObservedAt: input.ObservedAt, FetchedAt: input.FetchedAt, RecordedAt: s.now().UTC().Format(time.RFC3339Nano)}
		if !domain.SafeID(value.ObservationID) || !domain.CanonicalUTC(value.RecordedAt) {
			return errors.New("FX observation durable metadata is invalid")
		}
		if err := scope.InsertFX(ctx, value); err != nil {
			return fmt.Errorf("store FX observation: %w", err)
		}
		result = &value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) RecordPrice(ctx context.Context, input c.SecurityPriceObservationInput) (*c.SecurityPriceObservation, error) {
	if err := s.decode.Price(input); err != nil {
		return nil, err
	}
	var result *c.SecurityPriceObservation
	err := s.store.Write(ctx, func(scope Scope) error {
		stored, err := scope.PriceBySource(ctx, input.Source, input.SourceObservationID)
		if err == nil {
			fetched := stored.FetchedAt == input.FetchedAt || strings.HasPrefix(input.Source, "kiwoom_")
			if stored.Source != input.Source || stored.SourceObservationID != input.SourceObservationID || stored.InstrumentID != input.InstrumentID || stored.Symbol != input.Symbol || stored.Venue != input.Venue || stored.Currency != input.Currency || stored.Price != input.Price || stored.PriceAdjustment != input.PriceAdjustment || stored.ObservedAt != input.ObservedAt || !fetched {
				return errors.New("security price source observation identity is already bound to different data")
			}
			result = stored
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if strings.HasPrefix(input.Source, "kiwoom_") {
			listing, err := scope.ResolveListing(ctx, input.Venue, input.Symbol, input.Currency)
			if err != nil {
				return err
			}
			if listing.InstrumentID != input.InstrumentID {
				return errors.New("Kiwoom price observation instrument is not owner-declared")
			}
		}
		value := c.SecurityPriceObservation{ObservationID: s.id("security_price_observation"), Source: input.Source, SourceObservationID: input.SourceObservationID, InstrumentID: input.InstrumentID, Symbol: input.Symbol, Venue: input.Venue, Currency: input.Currency, Price: input.Price, PriceAdjustment: input.PriceAdjustment, ObservedAt: input.ObservedAt, FetchedAt: input.FetchedAt, RecordedAt: s.now().UTC().Format(time.RFC3339Nano)}
		if !domain.SafeID(value.ObservationID) || !domain.CanonicalUTC(value.RecordedAt) {
			return errors.New("security price observation durable metadata is invalid")
		}
		fetched, _ := time.Parse(time.RFC3339Nano, value.FetchedAt)
		recorded, _ := time.Parse(time.RFC3339Nano, value.RecordedAt)
		if recorded.Before(fetched) {
			return errors.New("security price observation recorded_at precedes fetched_at")
		}
		if err := scope.InsertPrice(ctx, value); err != nil {
			return fmt.Errorf("store security price observation: %w", err)
		}
		result = &value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ResolveListing(ctx context.Context, venue, symbol, currency string) (*c.InstrumentListingEvent, error) {
	var result *c.InstrumentListingEvent
	err := s.store.Read(ctx, func(scope Reader) error {
		var err error
		result, err = scope.ResolveListing(ctx, venue, symbol, currency)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) DeclareListing(ctx context.Context, input c.InstrumentListingInput) (*c.InstrumentListingEvent, error) {
	return s.recordListing(ctx, "DECLARE", input)
}
func (s *Service) RevokeListing(ctx context.Context, input c.InstrumentListingInput) (*c.InstrumentListingEvent, error) {
	return s.recordListing(ctx, "REVOKE", input)
}

func (s *Service) recordListing(ctx context.Context, eventType string, input c.InstrumentListingInput) (*c.InstrumentListingEvent, error) {
	if err := s.decode.Listing(input); err != nil {
		return nil, err
	}
	var result *c.InstrumentListingEvent
	err := s.store.Write(ctx, func(scope Scope) error {
		active, _, err := scope.Listings(ctx)
		if err != nil {
			return err
		}
		current, exists := active[input.Venue+"\x00"+input.Symbol+"\x00"+input.Currency]
		if eventType == "DECLARE" && exists {
			if current.InstrumentID != input.InstrumentID {
				return errors.New("instrument listing is already declared")
			}
			result = &current
			return nil
		}
		if eventType == "REVOKE" && (!exists || current.InstrumentID != input.InstrumentID) {
			return ErrListingNotFound
		}
		previous, err := scope.PreviousListing(ctx, input.Venue, input.Symbol, input.Currency)
		if err != nil {
			return err
		}
		value := c.InstrumentListingEvent{EventID: s.id("instrument_listing"), EventType: eventType, Authority: "owner_declared", InstrumentID: input.InstrumentID, Venue: input.Venue, Symbol: input.Symbol, Currency: input.Currency, ExpectedPreviousEventID: previous, RecordedAt: s.now().UTC().Format(time.RFC3339Nano)}
		if !domain.SafeID(value.EventID) || !domain.CanonicalUTC(value.RecordedAt) {
			return errors.New("instrument listing durable metadata is invalid")
		}
		if err := scope.InsertListing(ctx, value); err != nil {
			return fmt.Errorf("store instrument listing event: %w", err)
		}
		result = &value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

var ErrNotFound = c.ErrNotFound
var ErrListingNotFound = c.ErrListingNotFound

func (s *Service) LatestFX(ctx context.Context, source, base, quote, asOf string) (*c.FXObservation, error) {
	var result *c.FXObservation
	err := s.store.Read(ctx, func(scope Reader) error {
		var err error
		result, err = scope.LatestFX(ctx, source, base, quote, asOf)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
