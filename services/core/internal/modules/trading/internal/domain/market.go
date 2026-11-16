package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	"strings"
	"time"
)

const paperMarketBarSchema = "paper-market-bar.v1"

var safeOrderID = SafeOrderID
var orderAlias = OrderAlias
var validateMarketDataValues = ValidateBarValues

type PaperMarketBarObservation struct {
	ObservationID       string `json:"observation_id"`
	SchemaVersion       string `json:"schema_version"`
	Source              string `json:"source"`
	SourceObservationID string `json:"source_observation_id"`
	InputDataSHA256     string `json:"input_data_sha256"`
	Symbol              string `json:"symbol"`
	Venue               string `json:"venue"`
	Currency            string `json:"currency"`
	Interval            string `json:"interval"`
	Timezone            string `json:"timezone"`
	PriceAdjustment     string `json:"price_adjustment"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
	Volume              string `json:"volume"`
	OpenAt              string `json:"open_at"`
	CloseAt             string `json:"close_at"`
	SourceAvailableAt   string `json:"source_available_at"`
	FetchedAt           string `json:"fetched_at"`
	RecordedAt          string `json:"recorded_at"`
}
type PaperSignal struct {
	SchemaVersion            string `json:"schema_version"`
	SignalID                 string `json:"signal_id"`
	SignalBarObservationID   string `json:"signal_bar_observation_id,omitempty"`
	StrategyResultSHA256     string `json:"strategy_result_sha256"`
	StrategySelectionEventID string `json:"strategy_selection_event_id"`
	DataSHA256               string `json:"data_sha256"`
	Symbol                   string `json:"symbol"`
	TargetQuantity           string `json:"target_quantity"`
	DataAsOf                 string `json:"data_as_of"`
	GeneratedAt              string `json:"generated_at"`
	ExpiresAt                string `json:"expires_at"`
}
type PaperSignalEvent struct {
	EventID                         string `json:"event_id"`
	SchemaVersion                   string `json:"schema_version"`
	AccountRef                      string `json:"account_ref"`
	PaperAccountingSessionID        string `json:"paper_accounting_session_id"`
	StrategyResultSHA256            string `json:"strategy_result_sha256"`
	StrategySelectionEventID        string `json:"strategy_selection_event_id"`
	ExecutionPolicySHA256           string `json:"execution_policy_sha256"`
	SignalID                        string `json:"signal_id"`
	SignalBarObservationID          string `json:"signal_bar_observation_id"`
	DataSHA256                      string `json:"data_sha256"`
	Symbol                          string `json:"symbol"`
	TargetQuantity                  string `json:"target_quantity"`
	DataAsOf                        string `json:"data_as_of"`
	GeneratedAt                     string `json:"generated_at"`
	ExpiresAt                       string `json:"expires_at"`
	MarketObservationSequenceCutoff int64  `json:"market_observation_sequence_cutoff"`
	RecordedAt                      string `json:"recorded_at"`
}

func ValidatePaperMarketBar(bar PaperMarketBarObservation) error {
	openAt, openOK := parsePaperTime(bar.OpenAt)
	closeAt, closeOK := parsePaperTime(bar.CloseAt)
	availableAt, availableOK := parsePaperTime(bar.SourceAvailableAt)
	fetchedAt, fetchedOK := parsePaperTime(bar.FetchedAt)
	recordedAt, recordedOK := parsePaperTime(bar.RecordedAt)
	if !safeOrderID(bar.ObservationID) || bar.SchemaVersion != paperMarketBarSchema || bar.Source != "paper_fixture" ||
		!safeOrderID(bar.SourceObservationID) || !strategySHA256Pattern.MatchString(bar.InputDataSHA256) ||
		!kiwoomStockPattern.MatchString(bar.Symbol) || bar.Venue != "KRX" || bar.Currency != "KRW" || bar.Interval != "1d" ||
		bar.Timezone != "Asia/Seoul" || bar.PriceAdjustment != "unspecified" || !openOK || !closeOK || !availableOK ||
		!fetchedOK || !recordedOK || !openAt.Before(closeAt) || closeAt.After(availableAt) || availableAt.After(fetchedAt) ||
		fetchedAt.After(recordedAt) || !canonicalPaperTimes(bar.OpenAt, bar.CloseAt, bar.SourceAvailableAt, bar.FetchedAt, bar.RecordedAt) ||
		paperMarketBarObservationID(bar.Source, bar.SourceObservationID) != bar.ObservationID {
		return errors.New("paper market bar identity or time contract is invalid")
	}
	if err := validateMarketDataValues(bar.Open, bar.High, bar.Low, bar.Close, bar.Volume); err != nil {
		return fmt.Errorf("paper market bar %w", err)
	}
	return nil
}

var validatePaperMarketBar = ValidatePaperMarketBar

func ValidateCapitalizedPaperSignal(signal PaperSignal, now time.Time) error {
	if err := validateCapitalizedPaperSignalShape(signal); err != nil {
		return err
	}
	generatedAt, _ := parsePaperTime(signal.GeneratedAt)
	expiresAt, _ := parsePaperTime(signal.ExpiresAt)
	if generatedAt.After(now) || !now.Before(expiresAt) {
		return errors.New("capitalized paper signal is not active")
	}
	return nil
}

var validateCapitalizedPaperSignal = ValidateCapitalizedPaperSignal

func ValidateCapitalizedPaperSignalShape(signal PaperSignal) error {
	dataAsOf, dataOK := parsePaperTime(signal.DataAsOf)
	generatedAt, generatedOK := parsePaperTime(signal.GeneratedAt)
	expiresAt, expiresOK := parsePaperTime(signal.ExpiresAt)
	if signal.SchemaVersion != capitalizedPaperSignalSchema || !safeOrderID(signal.SignalID) || !safeOrderID(signal.SignalBarObservationID) ||
		!strategySHA256Pattern.MatchString(signal.StrategyResultSHA256) || !safeOrderID(signal.StrategySelectionEventID) ||
		!strategySHA256Pattern.MatchString(signal.DataSHA256) || !kiwoomStockPattern.MatchString(signal.Symbol) ||
		!validPaperTargetQuantity(signal.TargetQuantity) || !dataOK || !generatedOK || !expiresOK || dataAsOf.After(generatedAt) ||
		!generatedAt.Before(expiresAt) {
		return errors.New("capitalized paper signal is invalid")
	}
	return nil
}

var validateCapitalizedPaperSignalShape = ValidateCapitalizedPaperSignalShape

func ValidatePaperSignalEvent(event PaperSignalEvent) error {
	dataAsOf, dataOK := parsePaperTime(event.DataAsOf)
	generatedAt, generatedOK := parsePaperTime(event.GeneratedAt)
	expiresAt, expiresOK := parsePaperTime(event.ExpiresAt)
	recordedAt, recordedOK := parsePaperTime(event.RecordedAt)
	if !safeOrderID(event.EventID) || event.SchemaVersion != capitalizedPaperSignalSchema || !orderAlias(event.AccountRef, "account") ||
		!safeOrderID(event.PaperAccountingSessionID) || !strategySHA256Pattern.MatchString(event.StrategyResultSHA256) ||
		!safeOrderID(event.StrategySelectionEventID) || !strategySHA256Pattern.MatchString(event.ExecutionPolicySHA256) ||
		!safeOrderID(event.SignalID) || !safeOrderID(event.SignalBarObservationID) || !strategySHA256Pattern.MatchString(event.DataSHA256) ||
		!kiwoomStockPattern.MatchString(event.Symbol) || !validPaperTargetQuantity(event.TargetQuantity) || event.MarketObservationSequenceCutoff <= 0 ||
		!dataOK || !generatedOK || !expiresOK || !recordedOK || !canonicalPaperTimes(event.DataAsOf, event.GeneratedAt, event.ExpiresAt, event.RecordedAt) ||
		dataAsOf.After(generatedAt) || generatedAt.After(recordedAt) ||
		!generatedAt.Before(expiresAt) || !recordedAt.Before(expiresAt) || paperSignalEventID(event.AccountRef, event.SignalID) != event.EventID {
		return errors.New("paper signal event is invalid")
	}
	return nil
}

var validatePaperSignalEvent = ValidatePaperSignalEvent

func SamePaperMarketBarInput(left, right PaperMarketBarObservation) bool {
	left.ObservationID, left.SchemaVersion, left.RecordedAt = "", "", ""
	right.ObservationID, right.SchemaVersion, right.RecordedAt = "", "", ""
	return left == right
}

var samePaperMarketBarInput = SamePaperMarketBarInput

func SamePaperSignalInput(event PaperSignalEvent, signal PaperSignal) bool {
	return event.SchemaVersion == signal.SchemaVersion && event.StrategyResultSHA256 == signal.StrategyResultSHA256 &&
		event.StrategySelectionEventID == signal.StrategySelectionEventID && event.SignalID == signal.SignalID &&
		event.SignalBarObservationID == signal.SignalBarObservationID && event.DataSHA256 == signal.DataSHA256 &&
		event.Symbol == signal.Symbol && event.TargetQuantity == signal.TargetQuantity && event.DataAsOf == signal.DataAsOf &&
		event.GeneratedAt == signal.GeneratedAt && event.ExpiresAt == signal.ExpiresAt
}

var samePaperSignalInput = SamePaperSignalInput

func ValidPaperTargetQuantity(raw string) bool {
	return paperdomain.ValidCapitalizedQuantity(raw, true)
}

var validPaperTargetQuantity = ValidPaperTargetQuantity

func CanonicalPaperTime(raw string) (string, bool) {
	value, ok := parsePaperTime(raw)
	if !ok {
		return "", false
	}
	return value.Format(canonicalPaperTimeLayout), true
}

var canonicalPaperTime = CanonicalPaperTime

func ParsePaperTime(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || value.Location() != time.UTC {
		return time.Time{}, false
	}
	return value, value.Format(time.RFC3339Nano) == raw || value.Format(canonicalPaperTimeLayout) == raw
}

var parsePaperTime = ParsePaperTime

func CanonicalPaperTimes(values ...string) bool {
	for _, raw := range values {
		canonical, ok := canonicalPaperTime(raw)
		if !ok || canonical != raw {
			return false
		}
	}
	return true
}

var canonicalPaperTimes = CanonicalPaperTimes

func PaperMarketBarObservationID(source, sourceID string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{source, sourceID}, "\x00")))
	return "paper_market_bar_" + hex.EncodeToString(hash[:16])
}

var paperMarketBarObservationID = PaperMarketBarObservationID

func PaperSignalEventID(accountRef, signalID string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{accountRef, signalID}, "\x00")))
	return "paper_signal_event_" + hex.EncodeToString(hash[:16])
}

var paperSignalEventID = PaperSignalEventID

func SamePaperSnapshotBar(stored, supplied PaperMarketBarObservation) bool {
	stored.ObservationID, stored.SourceObservationID, stored.InputDataSHA256, stored.RecordedAt = "", "", "", ""
	supplied.ObservationID, supplied.SourceObservationID, supplied.InputDataSHA256, supplied.RecordedAt = "", "", "", ""
	return stored == supplied
}
