package sqlite

import (
	"context"
	"errors"
	marketcodec "omni-folio/services/core/internal/modules/marketdata/internal/adapters/codec"
	identity "omni-folio/services/core/internal/modules/marketdata/internal/adapters/ledger"
	app "omni-folio/services/core/internal/modules/marketdata/internal/application"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

const marketDataAdjustmentProviderAdjusted = "provider_adjusted"

func securityPriceInput() SecurityPriceObservationInput {
	return SecurityPriceObservationInput{
		Source: "local_fixture", SourceObservationID: "aapl_close_20260110", InstrumentID: "US0378331005",
		Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "250.25",
		PriceAdjustment: marketDataAdjustmentUnspecified, ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z",
	}
}

func TestSecurityPriceObservationAcceptsCanonicalInternalInstrumentID(t *testing.T) {
	svc, _ := priceTestService(t, []time.Time{priceMustTime("2026-01-10T15:02:00Z")}, nil)
	input := securityPriceInput()
	input.InstrumentID = "instrument_aapl"

	stored, err := svc.RecordPrice(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if stored.InstrumentID != "instrument_aapl" || stored.Symbol != "AAPL" || stored.Venue != "XNAS" {
		t.Fatalf("stored identity drifted: %+v", stored)
	}
	replayed, _, err := replaySecurityPriceObservations(context.Background(), svc.reader())
	if err != nil || len(replayed) != 1 || replayed[0].InstrumentID != "instrument_aapl" || replayed[0].Symbol != "AAPL" || replayed[0].Venue != "XNAS" {
		t.Fatalf("replayed identity drifted: observations=%+v err=%v", replayed, err)
	}
	latest, err := latestSecurityPriceObservation(context.Background(), svc.reader(), input.Source, input.InstrumentID, input.Symbol, input.Venue, input.Currency, input.PriceAdjustment, "2026-01-10T15:03:00Z")
	if err != nil || latest.SourceObservationID != input.SourceObservationID || latest.InstrumentID != "instrument_aapl" || latest.Symbol != "AAPL" || latest.Venue != "XNAS" {
		t.Fatalf("exact latest lookup drifted: observation=%+v err=%v", latest, err)
	}
}

func TestSecurityPriceObservationRecordReplayConflictAndSnapshotBoundary(t *testing.T) {
	svc, _ := priceTestService(t, nil, nil)
	ctx := context.Background()
	first := securityPriceInput()
	stored, err := svc.RecordPrice(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if !canonicalUTCString(stored.ObservedAt) || !canonicalUTCString(stored.FetchedAt) || !canonicalUTCString(stored.RecordedAt) {
		t.Fatalf("stored timestamps are not canonical UTC: %+v", stored)
	}
	replayed, err := svc.RecordPrice(ctx, first)
	if err != nil || !reflect.DeepEqual(stored, replayed) {
		t.Fatalf("exact replay drifted: stored=%+v replayed=%+v err=%v", stored, replayed, err)
	}
	changed := first
	changed.Price = "251"
	if _, err := svc.RecordPrice(ctx, changed); err == nil {
		t.Fatal("source observation identity was rebound to a different price")
	}
	sameSlot := first
	sameSlot.SourceObservationID = "aapl_close_other"
	if _, err := svc.RecordPrice(ctx, sameSlot); err == nil {
		t.Fatal("source/instrument/venue/currency/time/adjustment slot accepted two identities")
	}
	second := first
	second.SourceObservationID = "aapl_close_20260111"
	second.Price = "252"
	second.ObservedAt = "2026-01-10T15:00:02Z"
	second.FetchedAt = "2026-01-10T15:00:03Z"
	if _, err := svc.RecordPrice(ctx, second); err != nil {
		t.Fatal(err)
	}
	observations, proof, err := replaySecurityPriceObservations(ctx, svc.reader())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[0].SourceObservationID != first.SourceObservationID ||
		observations[1].SourceObservationID != second.SourceObservationID || observations[0].Price != first.Price ||
		observations[1].Price != second.Price || proof.Observations != 2 || len(proof.SHA256) != 64 {
		t.Fatalf("security price series/proof drifted: observations=%+v proof=%+v", observations, proof)
	}
	snapshot, err := svc.ledger.Snapshot(ctx)
	if err != nil || snapshot.ValuationStatus != "unavailable" {
		t.Fatalf("price storage changed valuation authority: snapshot=%+v err=%v", snapshot, err)
	}
}

func TestSecurityPriceObservationRejectsInvalidBoundaryAndDirectMutation(t *testing.T) {
	valid := securityPriceInput()
	tests := map[string]func(*SecurityPriceObservationInput){
		"empty source id":         func(v *SecurityPriceObservationInput) { v.SourceObservationID = "" },
		"unknown source":          func(v *SecurityPriceObservationInput) { v.Source = "kiwoom" },
		"empty instrument":        func(v *SecurityPriceObservationInput) { v.InstrumentID = "" },
		"lower symbol":            func(v *SecurityPriceObservationInput) { v.Symbol = "aapl" },
		"empty venue":             func(v *SecurityPriceObservationInput) { v.Venue = "" },
		"lower currency":          func(v *SecurityPriceObservationInput) { v.Currency = "usd" },
		"zero price":              func(v *SecurityPriceObservationInput) { v.Price = "0" },
		"negative price":          func(v *SecurityPriceObservationInput) { v.Price = "-1" },
		"exponent price":          func(v *SecurityPriceObservationInput) { v.Price = "1e3" },
		"trailing zero price":     func(v *SecurityPriceObservationInput) { v.Price = "250.250" },
		"unknown adjustment":      func(v *SecurityPriceObservationInput) { v.PriceAdjustment = "split_adjusted" },
		"provider adjustment":     func(v *SecurityPriceObservationInput) { v.PriceAdjustment = marketDataAdjustmentProviderAdjusted },
		"offset observed":         func(v *SecurityPriceObservationInput) { v.ObservedAt = "2026-01-11T00:00:00+09:00" },
		"offset fetched":          func(v *SecurityPriceObservationInput) { v.FetchedAt = "2026-01-11T00:00:01+09:00" },
		"fetched before observed": func(v *SecurityPriceObservationInput) { v.FetchedAt = "2026-01-10T14:59:59Z" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _ := priceTestService(t, nil, nil)
			observation := valid
			mutate(&observation)
			if _, err := svc.RecordPrice(context.Background(), observation); err == nil {
				t.Fatalf("invalid security price observation was accepted: %+v", observation)
			}
		})
	}
	recordedBeforeFetched, _ := priceTestService(t, []time.Time{priceMustTime("2026-01-10T15:00:00Z")}, nil)
	if _, err := recordedBeforeFetched.RecordPrice(context.Background(), valid); err == nil {
		t.Fatal("security price observation was recorded before its fetched_at timestamp")
	}

	svc, _ := priceTestService(t, nil, nil)
	var strict int
	if err := svc.db.QueryRow(`SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='security_price_observations'`).Scan(&strict); err != nil || strict != 1 {
		t.Fatalf("security price table is not STRICT: strict=%d err=%v", strict, err)
	}
	if _, err := svc.db.Exec(`INSERT INTO security_price_observations(observation_id,source,source_observation_id,instrument_id,symbol,venue,currency,price,price_adjustment,observed_at,fetched_at,record_sha256,recorded_at) VALUES('bad-price','local_fixture','bad-price','US0378331005','AAPL','XNAS','USD','250.250','unspecified','2026-01-10T15:00:00Z','2026-01-10T15:00:01Z','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','2026-01-10T15:00:02Z')`); err == nil {
		t.Fatal("STRICT storage accepted a non-canonical direct price")
	}
	if _, err := svc.RecordPrice(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE security_price_observations SET price=price`,
		`DELETE FROM security_price_observations`,
	} {
		if _, err := svc.db.Exec(statement); err == nil {
			t.Fatalf("insert-only security price storage accepted mutation: %s", statement)
		}
	}
}

func TestSecurityPriceObservationLatestIsExactAndExcludesFutureKnowledge(t *testing.T) {
	svc, _ := priceTestService(t, []time.Time{
		priceMustTime("2026-01-10T15:02:00Z"), priceMustTime("2026-01-10T15:03:00Z"), priceMustTime("2026-01-10T15:05:00Z"), priceMustTime("2026-01-10T15:07:00Z"),
	}, nil)
	current := securityPriceInput()
	futureRecorded := current
	futureRecorded.SourceObservationID = "aapl_future_recorded"
	futureRecorded.Price = "250.5"
	futureRecorded.ObservedAt = "2026-01-10T14:59:00Z"
	futureRecorded.FetchedAt = "2026-01-10T14:59:01Z"
	futureFetched := current
	futureFetched.SourceObservationID = "aapl_future_fetch"
	futureFetched.Price = "251"
	futureFetched.ObservedAt = "2026-01-10T15:01:00Z"
	futureFetched.FetchedAt = "2026-01-10T15:05:00Z"
	futureObserved := current
	futureObserved.SourceObservationID = "aapl_future_observed"
	futureObserved.Price = "252"
	futureObserved.ObservedAt = "2026-01-10T15:05:00Z"
	futureObserved.FetchedAt = "2026-01-10T15:05:01Z"
	for _, input := range []SecurityPriceObservationInput{current, futureRecorded, futureFetched, futureObserved} {
		if _, err := svc.RecordPrice(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	lookup := func(source, instrumentID, symbol, venue, currency, adjustment string) (*SecurityPriceObservation, error) {
		return latestSecurityPriceObservation(context.Background(), svc.reader(), source, instrumentID, symbol, venue, currency, adjustment, "2026-01-10T15:02:00Z")
	}
	got, err := lookup(current.Source, current.InstrumentID, current.Symbol, current.Venue, current.Currency, current.PriceAdjustment)
	if err != nil || got.SourceObservationID != current.SourceObservationID || got.Price != current.Price {
		t.Fatalf("exact as-of lookup drifted: observation=%+v err=%v", got, err)
	}
	for _, mismatch := range [][6]string{
		{"kiwoom", current.InstrumentID, current.Symbol, current.Venue, current.Currency, current.PriceAdjustment},
		{current.Source, "US5949181045", current.Symbol, current.Venue, current.Currency, current.PriceAdjustment},
		{current.Source, current.InstrumentID, "MSFT", current.Venue, current.Currency, current.PriceAdjustment},
		{current.Source, current.InstrumentID, current.Symbol, "XNYS", current.Currency, current.PriceAdjustment},
		{current.Source, current.InstrumentID, current.Symbol, current.Venue, "KRW", current.PriceAdjustment},
		{current.Source, current.InstrumentID, current.Symbol, current.Venue, current.Currency, marketDataAdjustmentProviderAdjusted},
	} {
		if observation, err := lookup(mismatch[0], mismatch[1], mismatch[2], mismatch[3], mismatch[4], mismatch[5]); err == nil || observation != nil {
			t.Fatalf("exact lookup accepted mismatched identity: observation=%+v err=%v", observation, err)
		}
	}
}

func TestSecurityPriceObservationReplayFailsClosedOnCorruption(t *testing.T) {
	svc, _ := priceTestService(t, nil, nil)
	if _, err := svc.RecordPrice(context.Background(), securityPriceInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER security_price_observations_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE security_price_observations SET price='251'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := replaySecurityPriceObservations(context.Background(), svc.reader()); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("corrupt security price observation was certified: %v", err)
	}
}

var strategySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type latestTradeFunc func(context.Context, string) (app.LatestTrade, error)

func (f latestTradeFunc) LatestTrade(ctx context.Context, symbol string) (app.LatestTrade, error) {
	return f(ctx, symbol)
}
func declareTestInstrumentListing(t *testing.T, svc *priceTestFixture, instrumentID string) {
	t.Helper()
	if _, err := svc.DeclareListing(context.Background(), InstrumentListingInput{InstrumentID: instrumentID, Venue: "XKRX", Symbol: "005930", Currency: "KRW"}); err != nil {
		t.Fatal(err)
	}
}
func TestKiwoomLatestTradeRecordsDurablePriceObservation(t *testing.T) {
	svc, _ := priceTestService(t, []time.Time{
		priceMustTime("2026-08-24T01:30:02Z"),
		priceMustTime("2026-08-24T01:30:03Z"),
	}, nil)
	declareTestInstrumentListing(t, svc, "instrument_005930")
	trade := app.LatestTrade{
		Source: "kiwoom", Environment: "production", Exchange: "KRX",
		Symbol: "005930", Currency: "KRW", Price: "1250",
		ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z",
	}

	stored, err := svc.RecordLatestTrade(context.Background(), trade)
	if err != nil {
		t.Fatal(err)
	}
	if stored.InstrumentID != "instrument_005930" {
		t.Fatalf("Kiwoom observation did not use the ledger instrument identity: %+v", stored)
	}
	replayed, err := svc.RecordLatestTrade(context.Background(), trade)
	if err != nil || !reflect.DeepEqual(stored, replayed) {
		t.Fatalf("exact Kiwoom observation replay drifted: stored=%+v replayed=%+v err=%v", stored, replayed, err)
	}
	nextFetch := trade
	nextFetch.FetchedAt = "2026-08-24T01:30:02Z"
	replayed, err = svc.RecordLatestTrade(context.Background(), nextFetch)
	if err != nil || !reflect.DeepEqual(stored, replayed) {
		t.Fatalf("same-slot Kiwoom observation replay drifted: stored=%+v replayed=%+v err=%v", stored, replayed, err)
	}
	changedPrice := nextFetch
	changedPrice.Price = "1251"
	if _, err := svc.RecordLatestTrade(context.Background(), changedPrice); err == nil {
		t.Fatal("same-second Kiwoom price ambiguity was stored")
	}
	directReplay := SecurityPriceObservationInput{
		Source: stored.Source, SourceObservationID: stored.SourceObservationID, InstrumentID: stored.InstrumentID,
		Symbol: stored.Symbol, Venue: stored.Venue, Currency: stored.Currency, Price: stored.Price,
		PriceAdjustment: stored.PriceAdjustment, ObservedAt: stored.ObservedAt, FetchedAt: nextFetch.FetchedAt,
	}
	if replayed, err = svc.RecordPrice(context.Background(), directReplay); err != nil || !reflect.DeepEqual(stored, replayed) {
		t.Fatalf("atomic same-slot replay drifted: stored=%+v replayed=%+v err=%v", stored, replayed, err)
	}
	tamperedID := directReplay
	tamperedID.SourceObservationID = strings.Repeat("0", 64)
	if _, err := svc.RecordPrice(context.Background(), tamperedID); err == nil {
		t.Fatal("Kiwoom observation accepted an identity not derived from its slot")
	}
	tamperedInstrument := directReplay
	tamperedInstrument.InstrumentID = "krx_005930"
	tamperedInstrument.SourceObservationID, _ = kiwoomLatestTradeObservationID(
		tamperedInstrument.Source, tamperedInstrument.InstrumentID, tamperedInstrument.Symbol,
		tamperedInstrument.Venue, tamperedInstrument.Currency, tamperedInstrument.PriceAdjustment,
		tamperedInstrument.ObservedAt,
	)
	if _, err := svc.RecordPrice(context.Background(), tamperedInstrument); err == nil {
		t.Fatal("Kiwoom observation accepted an instrument identity outside the ledger rule")
	}
	for name, mutate := range map[string]func(*SecurityPriceObservationInput){
		"symbol":   func(v *SecurityPriceObservationInput) { v.Symbol = "AAPL" },
		"venue":    func(v *SecurityPriceObservationInput) { v.Venue = "XNAS" },
		"currency": func(v *SecurityPriceObservationInput) { v.Currency = "USD" },
	} {
		t.Run("direct_"+name, func(t *testing.T) {
			badMarket := directReplay
			mutate(&badMarket)
			badMarket.SourceObservationID, _ = kiwoomLatestTradeObservationID(
				badMarket.Source, badMarket.InstrumentID, badMarket.Symbol, badMarket.Venue,
				badMarket.Currency, badMarket.PriceAdjustment, badMarket.ObservedAt,
			)
			if _, err := svc.RecordPrice(context.Background(), badMarket); err == nil {
				t.Fatalf("direct Kiwoom observation accepted an invalid market identity: %+v", badMarket)
			}
		})
	}
	mockTrade := trade
	mockTrade.Environment = "mock"
	if _, err := svc.RecordLatestTrade(context.Background(), mockTrade); err != nil {
		t.Fatal(err)
	}
	invalidTrades := map[string]func(*app.LatestTrade){
		"source":      func(v *app.LatestTrade) { v.Source = "other" },
		"environment": func(v *app.LatestTrade) { v.Environment = "other" },
		"exchange":    func(v *app.LatestTrade) { v.Exchange = "NXT" },
		"symbol":      func(v *app.LatestTrade) { v.Symbol = "AAPL" },
		"currency":    func(v *app.LatestTrade) { v.Currency = "USD" },
		"price":       func(v *app.LatestTrade) { v.Price = "0" },
		"observed_at": func(v *app.LatestTrade) { v.ObservedAt = "not-a-time" },
	}
	for name, mutate := range invalidTrades {
		t.Run(name, func(t *testing.T) {
			invalid := trade
			mutate(&invalid)
			if _, err := svc.RecordLatestTrade(context.Background(), invalid); err == nil {
				t.Fatalf("invalid Kiwoom trade was recorded: %+v", invalid)
			}
		})
	}
	observations, proof, err := replaySecurityPriceObservations(context.Background(), svc.reader())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 || proof.Observations != 2 || observations[0].Source != "kiwoom_production" ||
		observations[1].Source != "kiwoom_mock" || observations[0].SourceObservationID == observations[1].SourceObservationID ||
		!strategySHA256Pattern.MatchString(observations[0].SourceObservationID) || observations[0].Venue != "XKRX" ||
		observations[0].PriceAdjustment != marketDataAdjustmentUnspecified {
		t.Fatalf("Kiwoom durable observation contract drifted: observations=%+v proof=%+v", observations, proof)
	}
	if latest, err := latestSecurityPriceObservation(context.Background(), svc.reader(), "kiwoom_production", "instrument_005930", "005930", "XKRX", "KRW", marketDataAdjustmentUnspecified, "2026-08-24T01:30:03Z"); err == nil || latest != nil {
		t.Fatalf("Kiwoom observation became a public/local-fixture valuation source: latest=%+v err=%v", latest, err)
	}
}

func TestKiwoomCaptureRequiresActiveListingBeforeNetwork(t *testing.T) {
	svc, _ := priceTestService(t, []time.Time{
		priceMustTime("2026-08-24T01:30:04Z"), priceMustTime("2026-08-24T01:30:05Z"),
	}, nil)
	networkCalls := 0
	client := latestTradeFunc(func(context.Context, string) (app.LatestTrade, error) {
		networkCalls++
		return app.LatestTrade{}, errors.New("unexpected provider call")
	})
	if observation, err := svc.CaptureLatestTrade(context.Background(), client, "005930"); err == nil || observation != nil {
		t.Fatalf("unlisted capture was accepted: observation=%+v err=%v", observation, err)
	}
	if networkCalls != 0 {
		t.Fatalf("unlisted capture reached network %d times", networkCalls)
	}

	listing := InstrumentListingInput{InstrumentID: "instrument_005930", Venue: "XKRX", Symbol: "005930", Currency: "KRW"}
	if _, err := svc.DeclareListing(context.Background(), listing); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeListing(context.Background(), listing); err != nil {
		t.Fatal(err)
	}
	if observation, err := svc.CaptureLatestTrade(context.Background(), client, "005930"); err == nil || observation != nil {
		t.Fatalf("revoked capture was accepted: observation=%+v err=%v", observation, err)
	}
	if networkCalls != 0 {
		t.Fatalf("revoked capture reached network %d times", networkCalls)
	}
	if _, err := svc.DeclareListing(context.Background(), listing); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER instrument_listing_events_no_update;
		UPDATE instrument_listing_events SET record_sha256=? WHERE event_id=(
			SELECT event_id FROM instrument_listing_events ORDER BY sequence DESC LIMIT 1
		)`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if observation, err := svc.CaptureLatestTrade(context.Background(), client, "005930"); err == nil || observation != nil {
		t.Fatalf("corrupt listing capture was accepted: observation=%+v err=%v", observation, err)
	}
	if networkCalls != 0 {
		t.Fatalf("corrupt listing capture reached network %d times", networkCalls)
	}
}

func TestKiwoomListingControlsInstrumentAndWriteRace(t *testing.T) {
	trade := app.LatestTrade{
		Source: "kiwoom", Environment: "production", Exchange: "KRX",
		Symbol: "005930", Currency: "KRW", Price: "1250",
		ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z",
	}
	svc, _ := priceTestService(t, []time.Time{
		priceMustTime("2026-08-24T01:30:04Z"), priceMustTime("2026-08-24T01:30:05Z"),
	}, nil)
	declareTestInstrumentListing(t, svc, "instrument_samsung_equity")
	stored, err := svc.RecordLatestTrade(context.Background(), trade)
	if err != nil || stored.InstrumentID != "instrument_samsung_equity" {
		t.Fatalf("owner-declared instrument was not used: observation=%+v err=%v", stored, err)
	}
	mismatched := SecurityPriceObservationInput{
		Source: "kiwoom_production", InstrumentID: "instrument_005930", Symbol: "005930", Venue: "XKRX", Currency: "KRW",
		Price: "1251", PriceAdjustment: marketDataAdjustmentUnspecified,
		ObservedAt: "2026-08-24T01:30:02Z", FetchedAt: "2026-08-24T01:30:03Z",
	}
	mismatched.SourceObservationID, _ = kiwoomLatestTradeObservationID(
		mismatched.Source, mismatched.InstrumentID, mismatched.Symbol, mismatched.Venue,
		mismatched.Currency, mismatched.PriceAdjustment, mismatched.ObservedAt,
	)
	if observation, err := svc.RecordPrice(context.Background(), mismatched); err == nil || observation != nil {
		t.Fatalf("direct Kiwoom writer bypassed listing ownership: observation=%+v err=%v", observation, err)
	}

	raceSvc, _ := priceTestService(t, []time.Time{
		priceMustTime("2026-08-24T01:30:04Z"), priceMustTime("2026-08-24T01:30:05Z"),
	}, nil)
	raceListing := InstrumentListingInput{InstrumentID: "instrument_005930", Venue: "XKRX", Symbol: "005930", Currency: "KRW"}
	if _, err := raceSvc.DeclareListing(context.Background(), raceListing); err != nil {
		t.Fatal(err)
	}
	client := latestTradeFunc(func(context.Context, string) (app.LatestTrade, error) {
		if _, err := raceSvc.RevokeListing(context.Background(), raceListing); err != nil {
			t.Fatal(err)
		}
		return trade, nil
	})
	if observation, err := raceSvc.CaptureLatestTrade(context.Background(), client, "005930"); err == nil || observation != nil {
		t.Fatalf("capture stored after listing revocation: observation=%+v err=%v", observation, err)
	}
	observations, _, err := replaySecurityPriceObservations(context.Background(), raceSvc.reader())
	if err != nil || len(observations) != 0 {
		t.Fatalf("revocation race left price evidence: observations=%+v err=%v", observations, err)
	}
}

func TestLatestTradeCaptureKeepsKnownGoodAndDoesNotRetryProvider(t *testing.T) {
	svc, _ := priceTestService(t, []time.Time{priceMustTime("2026-08-24T01:30:02Z")}, nil)
	declareTestInstrumentListing(t, svc, "instrument_005930")
	trade := app.LatestTrade{Source: "kiwoom", Environment: "production", Exchange: "KRX", Symbol: "005930", Currency: "KRW", Price: "1250", ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z"}
	source := latestTradeFunc(func(context.Context, string) (app.LatestTrade, error) { return trade, nil })
	stored, err := svc.CaptureLatestTrade(context.Background(), source, "005930")
	if err != nil {
		t.Fatal(err)
	}
	before, proof, err := replaySecurityPriceObservations(context.Background(), svc.reader())
	if err != nil || len(before) != 1 || before[0].Source != "kiwoom_production" || before[0].InstrumentID != "instrument_005930" {
		t.Fatalf("captured observation=%+v proof=%+v err=%v", before, proof, err)
	}
	trade.FetchedAt = "2026-08-24T01:30:03Z"
	replayed, err := svc.CaptureLatestTrade(context.Background(), source, "005930")
	if err != nil || !reflect.DeepEqual(stored, replayed) {
		t.Fatalf("same-slot capture drifted: stored=%+v replayed=%+v err=%v", stored, replayed, err)
	}
	calls := 0
	failed := latestTradeFunc(func(context.Context, string) (app.LatestTrade, error) {
		calls++
		return app.LatestTrade{}, errors.New("provider failure")
	})
	if observation, err := svc.CaptureLatestTrade(context.Background(), failed, "005930"); err == nil || observation != nil || calls != 1 {
		t.Fatalf("provider failure was retried or stored: observation=%+v calls=%d err=%v", observation, calls, err)
	}
	trade.Price = "1251"
	trade.FetchedAt = "2026-08-24T01:30:04Z"
	if observation, err := svc.CaptureLatestTrade(context.Background(), source, "005930"); err == nil || observation != nil {
		t.Fatalf("same-slot price conflict was stored: observation=%+v err=%v", observation, err)
	}
	after, afterProof, err := replaySecurityPriceObservations(context.Background(), svc.reader())
	if err != nil || !reflect.DeepEqual(before, after) || proof != afterProof {
		t.Fatalf("failed capture changed known-good evidence: before=%+v after=%+v proof=%+v after_proof=%+v err=%v", before, after, proof, afterProof, err)
	}
	calls = 0
	for _, input := range []struct {
		source app.LatestTradeSource
		symbol string
	}{{nil, "005930"}, {failed, "A005930"}} {
		if observation, err := svc.CaptureLatestTrade(context.Background(), input.source, input.symbol); err == nil || observation != nil {
			t.Fatalf("invalid capture identity was accepted: input=%+v observation=%+v err=%v", input, observation, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid capture identity reached provider %d times", calls)
	}
}

type priceWriteBarrier struct {
	app.Repository
	before func(context.Context) error
}

func (s priceWriteBarrier) Write(ctx context.Context, work func(app.Scope) error) error {
	if err := s.before(ctx); err != nil {
		return err
	}
	return s.Repository.Write(ctx, work)
}
func TestLatestTradeRechecksInstrumentInsidePriceTransaction(t *testing.T) {
	svc, _ := priceTestService(t, []time.Time{priceMustTime("2026-08-24T01:30:04Z")}, nil)
	declareTestInstrumentListing(t, svc, "instrument_005930")
	original := svc.Service
	store := New(svc.db, nil, nil, svc.ledger.CanonicalInstrumentID)
	repository := priceWriteBarrier{Repository: store, before: func(ctx context.Context) error {
		if _, err := original.RevokeListing(ctx, InstrumentListingInput{InstrumentID: "instrument_005930", Venue: "XKRX", Symbol: "005930", Currency: "KRW"}); err != nil {
			return err
		}
		_, err := original.DeclareListing(ctx, InstrumentListingInput{InstrumentID: "instrument_reassigned", Venue: "XKRX", Symbol: "005930", Currency: "KRW"})
		return err
	}}
	svc.Service = app.New(repository, marketcodec.New(svc.ledger.CanonicalInstrumentID), identity.New(svc.ledger), func() time.Time { return priceMustTime("2026-08-24T01:30:04Z") }, func(string) string { return "must_not_be_written" })
	trade := app.LatestTrade{Source: "kiwoom", Environment: "production", Exchange: "KRX", Symbol: "005930", Currency: "KRW", Price: "1250", ObservedAt: "2026-08-24T01:30:00Z", FetchedAt: "2026-08-24T01:30:01Z"}
	if value, err := svc.RecordLatestTrade(context.Background(), trade); err == nil || value != nil || !strings.Contains(err.Error(), "not owner-declared") {
		t.Fatalf("stale listing bypassed atomic price recheck: %+v %v", value, err)
	}
	observations, _, err := replaySecurityPriceObservations(context.Background(), svc.reader())
	if err != nil || len(observations) != 0 {
		t.Fatalf("reassignment race persisted price evidence: %+v %v", observations, err)
	}
}
