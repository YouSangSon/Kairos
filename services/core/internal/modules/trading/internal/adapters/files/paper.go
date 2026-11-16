package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type PaperMarketBarObservation = application.PaperMarketBarObservation

const maxMarketDataBytes = 1 << 20
const maxMarketDataRows = 500
const canonicalPaperTimeLayout = "2006-01-02T15:04:05.000000000Z"
const paperMarketBarSchema = "paper-market-bar.v1"
const marketDataAdjustmentUnspecified = "unspecified"

var paperEventID = domain.PaperEventID
var paperMarketBarObservationID = domain.PaperMarketBarObservationID
var validateMarketDataValues = domain.ValidateBarValues
var validatePaperMarketBar = application.ValidatePaperMarketBar
var paperResearchWholeSecond = application.PaperResearchWholeSecond
var paperSnapshotCSVHeader = []string{
	"bar_at", "symbol", "venue", "timezone", "interval", "open", "high", "low", "close", "volume",
	"open_at", "source_available_at", "fetched_at",
}

func ParsePaperSnapshot(raw []byte, clock func() time.Time) ([]PaperMarketBarObservation, string, error) {
	if len(raw) > maxMarketDataBytes {
		return nil, "", fmt.Errorf("paper snapshot must not exceed %d bytes", maxMarketDataBytes)
	}
	records, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		return nil, "", errors.New("paper snapshot must be valid CSV")
	}
	if len(records) < 2 || !reflect.DeepEqual(records[0], paperSnapshotCSVHeader) {
		return nil, "", errors.New("paper snapshot requires the canonical header and 1 to 500 rows")
	}
	if len(records)-1 > maxMarketDataRows {
		return nil, "", fmt.Errorf("paper snapshot must not contain more than %d rows", maxMarketDataRows)
	}

	hash := sha256.Sum256(raw)
	inputSHA := hex.EncodeToString(hash[:])
	now := clock().UTC()
	bars := make([]PaperMarketBarObservation, 0, len(records)-1)
	for index, row := range records[1:] {
		bar, err := paperSnapshotBar(row, inputSHA, now)
		if err != nil {
			return nil, "", fmt.Errorf("paper snapshot row %d: %w", index+2, err)
		}
		if index > 0 {
			if bar.Symbol != bars[0].Symbol || bar.Venue != bars[0].Venue || bar.Timezone != bars[0].Timezone || bar.Interval != bars[0].Interval {
				return nil, "", fmt.Errorf("paper snapshot row %d has inconsistent metadata", index+2)
			}
			if bar.CloseAt <= bars[index-1].CloseAt {
				return nil, "", fmt.Errorf("paper snapshot row %d is not strictly ordered by bar_at", index+2)
			}
		}
		bars = append(bars, bar)
	}

	return bars, inputSHA, nil
}

var parsePaperSnapshot = ParsePaperSnapshot

func PaperSnapshotBar(row []string, inputSHA string, now time.Time) (PaperMarketBarObservation, error) {
	if len(row) != len(paperSnapshotCSVHeader) {
		return PaperMarketBarObservation{}, errors.New("has the wrong number of fields")
	}
	if !kiwoomStockPattern.MatchString(row[1]) || row[2] != "KRX" || row[3] != "Asia/Seoul" || row[4] != "1d" {
		return PaperMarketBarObservation{}, errors.New("has invalid metadata")
	}
	if err := validateMarketDataValues(row[5], row[6], row[7], row[8], row[9]); err != nil {
		return PaperMarketBarObservation{}, err
	}
	parsedTimes := make([]time.Time, 4)
	for index, raw := range []string{row[10], row[0], row[11], row[12]} {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil || value.Location() != time.UTC || value.Nanosecond() != 0 || value.Format(time.RFC3339) != raw {
			return PaperMarketBarObservation{}, errors.New("requires UTC whole-second timestamps")
		}
		parsedTimes[index] = value
	}
	if !parsedTimes[0].Before(parsedTimes[1]) || parsedTimes[1].After(parsedTimes[2]) || parsedTimes[2].After(parsedTimes[3]) || parsedTimes[3].After(now) {
		return PaperMarketBarObservation{}, errors.New("has invalid market-data timing")
	}
	closeAt := parsedTimes[1].Format(canonicalPaperTimeLayout)
	bar := PaperMarketBarObservation{
		ObservationID: paperMarketBarObservationID("paper_fixture", paperEventID("snapshot_bar", row[1], closeAt)),
		SchemaVersion: paperMarketBarSchema, Source: "paper_fixture", SourceObservationID: paperEventID("snapshot_bar", row[1], closeAt),
		InputDataSHA256: inputSHA, Symbol: row[1], Venue: row[2], Currency: "KRW", Interval: row[4], Timezone: row[3],
		PriceAdjustment: marketDataAdjustmentUnspecified, Open: row[5], High: row[6], Low: row[7], Close: row[8], Volume: row[9],
		OpenAt: parsedTimes[0].Format(canonicalPaperTimeLayout), CloseAt: closeAt,
		SourceAvailableAt: parsedTimes[2].Format(canonicalPaperTimeLayout), FetchedAt: parsedTimes[3].Format(canonicalPaperTimeLayout),
		RecordedAt: now.Format(canonicalPaperTimeLayout),
	}
	if err := validatePaperMarketBar(bar); err != nil {
		return PaperMarketBarObservation{}, err
	}
	return bar, nil
}

var paperSnapshotBar = PaperSnapshotBar

func ValidatePaperResearchInput(raw []byte, expectedSHA, symbol, proposalAsOf string) error {
	if len(raw) > maxMarketDataBytes {
		return fmt.Errorf("paper research input must not exceed %d bytes", maxMarketDataBytes)
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expectedSHA {
		return errors.New("paper research input SHA-256 mismatch")
	}
	if !utf8.Valid(raw) || !kiwoomStockPattern.MatchString(symbol) {
		return errors.New("paper research input metadata is invalid")
	}
	records, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		return errors.New("paper research input must be valid CSV with fixed row widths")
	}
	if len(records) < 2 {
		return errors.New("paper research input requires a header and at least one row")
	}
	if len(records)-1 > maxMarketDataRows {
		return fmt.Errorf("paper research input must not contain more than %d rows", maxMarketDataRows)
	}
	columns := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		if _, exists := columns[name]; exists {
			return errors.New("paper research input column names must be unique")
		}
		columns[name] = index
	}
	required := []string{"bar_at", "symbol", "open", "high", "low", "close", "volume"}
	for _, name := range required {
		if _, exists := columns[name]; !exists {
			return errors.New("paper research input lacks a required column")
		}
	}

	var last time.Time
	for rowIndex, row := range records[1:] {
		if row[columns["symbol"]] != symbol {
			return fmt.Errorf("paper research input row %d has a mismatched symbol", rowIndex+2)
		}
		at, ok := paperResearchWholeSecond(row[columns["bar_at"]])
		if !ok || (!last.IsZero() && !at.After(last)) {
			return fmt.Errorf("paper research input row %d has an invalid or unordered bar_at", rowIndex+2)
		}
		last = at
		if err := validateMarketDataValues(row[columns["open"]], row[columns["high"]], row[columns["low"]], row[columns["close"]], row[columns["volume"]]); err != nil {
			return fmt.Errorf("paper research input row %d: %w", rowIndex+2, err)
		}
	}
	proposalAt, ok := paperResearchWholeSecond(proposalAsOf)
	if !ok || !proposalAt.After(last) {
		return errors.New("paper proposal must be a UTC whole-second timestamp after the research sample")
	}
	return nil
}

var validatePaperResearchInput = ValidatePaperResearchInput

type Inputs struct{}

func (Inputs) Snapshot(raw []byte, clock func() time.Time) ([]application.PaperMarketBarObservation, string, error) {
	return ParsePaperSnapshot(raw, clock)
}
func (Inputs) Proposal(raw []byte) (map[string]any, error)       { return DecodePaperProposal(raw) }
func (Inputs) Bundle(raw []byte) ([]byte, []byte, []byte, error) { return DecodePaperInputBundle(raw) }
func (Inputs) Research(raw []byte, sha, symbol, asOf string) error {
	return ValidatePaperResearchInput(raw, sha, symbol, asOf)
}
func ParsePaperPromotionBars(ctx context.Context, raw []byte) (string, []researchdomain.Bar, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if len(raw) == 0 || len(raw) > maxMarketDataBytes || !utf8.Valid(raw) {
		return "", nil, errors.New("paper promotion bars require UTF-8 CSV of at most 1 MiB")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = 7
	header, err := reader.Read()
	if err != nil || !slices.Equal(header, []string{"bar_at", "symbol", "open", "high", "low", "close", "volume"}) {
		return "", nil, errors.New("paper promotion bars require the canonical seven-column header")
	}
	var symbol string
	var previous time.Time
	bars := make([]researchdomain.Bar, 0)
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("paper promotion bars CSV: %w", err)
		}
		if len(bars) == maxMarketDataRows {
			return "", nil, errors.New("paper promotion bars must not exceed 500 rows")
		}
		if !kiwoomStockPattern.MatchString(row[1]) || symbol != "" && row[1] != symbol {
			return "", nil, errors.New("paper promotion bars require exactly one six-digit stock symbol")
		}
		at, err := time.Parse(time.RFC3339, row[0])
		if err != nil || at.UTC().Format(time.RFC3339) != row[0] || len(bars) > 0 && !at.After(previous) {
			return "", nil, errors.New("paper promotion bars require strictly increasing whole-second UTC timestamps")
		}
		whole, fraction, _ := strings.Cut(row[5], ".")
		if len(whole) > 12 || len(fraction) > 8 || len(row[5]) > 21 {
			return "", nil, errors.New("paper promotion close is outside the paper SMA bounds")
		}
		if err := validateMarketDataValues(row[2], row[3], row[4], row[5], row[6]); err != nil {
			return "", nil, fmt.Errorf("paper promotion bars: %w", err)
		}
		symbol, previous = row[1], at
		bars = append(bars, researchdomain.Bar{At: row[0], Open: row[2], Close: row[5], Volume: row[6]})
	}
	if len(bars) == 0 {
		return "", nil, errors.New("paper promotion bars require at least one row")
	}
	return symbol, bars, nil
}

func (Inputs) Promotion(ctx context.Context, raw []byte) (string, []researchdomain.Bar, error) {
	return ParsePaperPromotionBars(ctx, raw)
}
