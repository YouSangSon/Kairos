package ledgerdomain

import (
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type BrokerPositionDifference struct {
	Symbol         string `json:"symbol"`
	BrokerQuantity string `json:"broker_quantity"`
	LedgerQuantity string `json:"ledger_quantity"`
	Difference     string `json:"difference"`
	Match          bool   `json:"match"`
}

type BrokerTotals struct {
	PurchaseAmount      string `json:"purchase_amount"`
	EvaluationAmount    string `json:"evaluation_amount"`
	UnrealizedPnL       string `json:"unrealized_pnl"`
	ReturnRatePercent   string `json:"return_rate_percent"`
	EstimatedAssets     string `json:"estimated_assets"`
	LoanAmount          string `json:"loan_amount"`
	CreditLoanAmount    string `json:"credit_loan_amount"`
	CreditLendingAmount string `json:"credit_lending_amount"`
}

type BrokerPosition struct {
	Symbol               string `json:"symbol"`
	Name                 string `json:"name"`
	Quantity             string `json:"quantity"`
	TradableQuantity     string `json:"tradable_quantity"`
	AveragePurchasePrice string `json:"average_purchase_price"`
	CurrentPrice         string `json:"current_price"`
	PurchaseAmount       string `json:"purchase_amount"`
	EvaluationAmount     string `json:"evaluation_amount"`
	UnrealizedPnL        string `json:"unrealized_pnl"`
	ReturnRatePercent    string `json:"return_rate_percent"`
	WeightPercent        string `json:"weight_percent"`
}

type BrokerOpenOrder struct {
	OrderRef          string `json:"order_ref"`
	Symbol            string `json:"symbol"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	Side              string `json:"side"`
	Quantity          string `json:"quantity"`
	Price             string `json:"price"`
	RemainingQuantity string `json:"remaining_quantity"`
	Time              string `json:"time"`
	Exchange          string `json:"exchange"`
}

type BrokerSnapshot struct {
	Source        string            `json:"source"`
	Environment   BrokerEnvironment `json:"environment"`
	Exchange      BrokerExchange    `json:"exchange"`
	AccountRef    string            `json:"account_ref"`
	MaskedAccount string            `json:"masked_account"`
	FetchedAt     string            `json:"fetched_at"`
	Complete      bool              `json:"complete"`
	Currency      string            `json:"currency"`
	Totals        BrokerTotals      `json:"totals"`
	Positions     []BrokerPosition  `json:"positions"`
	OpenOrders    []BrokerOpenOrder `json:"open_orders"`
}

type BrokerEnvironment string
type BrokerExchange string
type BrokerReconciliationRecord struct {
	ReconciliationID    string                     `json:"reconciliation_id"`
	SnapshotID          string                     `json:"snapshot_id"`
	LedgerAccountID     string                     `json:"ledger_account_id"`
	LedgerRevision      string                     `json:"ledger_revision"`
	RecordedAt          string                     `json:"recorded_at"`
	AllPositionsMatch   bool                       `json:"all_positions_match"`
	PositionDifferences []BrokerPositionDifference `json:"position_differences"`
}

const (
	BrokerProduction BrokerEnvironment = "production"
	BrokerMock       BrokerEnvironment = "mock"
	BrokerKRX        BrokerExchange    = "KRX"
)

var maskedKiwoomAccountPattern = regexp.MustCompile(`^\*{6}[0-9]{4}$`)
var ledgerRevisionPattern = regexp.MustCompile(`^rev_[0-9]{10}$`)
var kiwoomStockPattern = regexp.MustCompile(`^[0-9]{6}$`)
var orderAliasPattern = regexp.MustCompile(`^kiwoom_(account|order|execution)_[A-Za-z0-9_-]{24}$`)

func BrokerAccountAlias(raw string) bool { return orderAlias(raw, "account") }
func orderAlias(raw, kind string) bool {
	return orderAliasPattern.MatchString(raw) && strings.HasPrefix(raw, "kiwoom_"+kind+"_")
}
func kiwoomValidTime(raw string) bool {
	if !kiwoomStockPattern.MatchString(raw) {
		return false
	}
	_, err := time.Parse("150405", raw)
	return err == nil
}
func kiwoomText(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > 80 {
		return "", false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return v, true
}
func canonicalBrokerDecimal(raw string, nonNegative bool) bool {
	if len(raw) == 0 || len(raw) > 64 {
		return false
	}
	value, err := exact.ParseDecimal(raw)
	if err != nil || (nonNegative && value.Sign() < 0) {
		return false
	}
	canonical, err := exact.FormatDecimal(value)
	return err == nil && canonical == raw
}
func safeBrokerID(raw string) bool {
	if raw == "" || len(raw) > 128 || !utf8.ValidString(raw) {
		return false
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

type BrokerLedgerEvent struct{ Type, Symbol, Quantity, OccurredAt string }

func BrokerQuantities(events []BrokerLedgerEvent) (map[string]*big.Rat, error) {
	quantities := map[string]*big.Rat{}
	for _, event := range events {
		typ, symbol, raw, occurredAt := event.Type, event.Symbol, event.Quantity, event.OccurredAt
		if !CanonicalUTCString(occurredAt) {
			return nil, errors.New("broker ledger occurred_at is not canonical UTC")
		}
		if !kiwoomStockPattern.MatchString(symbol) {
			continue
		}
		quantity, err := exact.ParseDecimal(raw)
		if err != nil || quantity.Sign() <= 0 {
			return nil, fmt.Errorf("ledger quantity for %s is invalid", symbol)
		}
		if quantities[symbol] == nil {
			quantities[symbol] = new(big.Rat)
		}
		if typ == "BUY" {
			quantities[symbol].Add(quantities[symbol], quantity)
		} else {
			quantities[symbol].Sub(quantities[symbol], quantity)
			if quantities[symbol].Sign() < 0 {
				return nil, fmt.Errorf("ledger position for %s is negative", symbol)
			}
		}
	}
	return quantities, nil
}
func SameBrokerSnapshotIdentity(left, right *BrokerSnapshot) bool {
	return left != nil && right != nil && left.Source == right.Source && left.Environment == right.Environment && left.Exchange == right.Exchange && left.AccountRef == right.AccountRef && left.FetchedAt == right.FetchedAt
}
func SamePositionDifferences(left, right []BrokerPositionDifference) bool {
	if (left == nil) != (right == nil) || len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
func PositionDifferences(positions []BrokerPosition, ledger map[string]*big.Rat) ([]BrokerPositionDifference, bool, error) {
	broker := make(map[string]*big.Rat, len(positions))
	for _, position := range positions {
		quantity, err := exact.ParseDecimal(position.Quantity)
		if err != nil {
			return nil, false, err
		}
		broker[position.Symbol] = quantity
	}
	symbols := make(map[string]struct{}, len(broker)+len(ledger))
	for symbol := range broker {
		symbols[symbol] = struct{}{}
	}
	for symbol := range ledger {
		symbols[symbol] = struct{}{}
	}
	ordered := make([]string, 0, len(symbols))
	for symbol := range symbols {
		ordered = append(ordered, symbol)
	}
	sort.Strings(ordered)
	result := make([]BrokerPositionDifference, 0, len(ordered))
	allMatch := true
	for _, symbol := range ordered {
		brokerQuantity, ledgerQuantity := broker[symbol], ledger[symbol]
		if brokerQuantity == nil {
			brokerQuantity = new(big.Rat)
		}
		if ledgerQuantity == nil {
			ledgerQuantity = new(big.Rat)
		}
		difference := new(big.Rat).Sub(new(big.Rat).Set(brokerQuantity), ledgerQuantity)
		brokerRaw, err := exact.FormatDecimal(brokerQuantity)
		if err != nil {
			return nil, false, err
		}
		ledgerRaw, err := exact.FormatDecimal(ledgerQuantity)
		if err != nil {
			return nil, false, err
		}
		differenceRaw, err := exact.FormatDecimal(difference)
		if err != nil {
			return nil, false, err
		}
		match := difference.Sign() == 0
		allMatch = allMatch && match
		result = append(result, BrokerPositionDifference{symbol, brokerRaw, ledgerRaw, differenceRaw, match})
	}
	return result, allMatch, nil
}

func ValidateBrokerSnapshot(snapshot *BrokerSnapshot) error {
	if snapshot == nil || !snapshot.Complete || snapshot.Source != "kiwoom" ||
		(snapshot.Environment != BrokerProduction && snapshot.Environment != BrokerMock) || snapshot.Exchange != BrokerKRX ||
		!orderAlias(snapshot.AccountRef, "account") || !maskedKiwoomAccountPattern.MatchString(snapshot.MaskedAccount) ||
		snapshot.Currency != "KRW" || snapshot.Positions == nil || snapshot.OpenOrders == nil {
		return errors.New("broker snapshot identity or completeness is invalid")
	}
	if !CanonicalUTCString(snapshot.FetchedAt) {
		return errors.New("broker snapshot fetched_at must be canonical UTC")
	}
	for _, value := range []string{snapshot.Totals.UnrealizedPnL, snapshot.Totals.ReturnRatePercent, snapshot.Totals.EstimatedAssets} {
		if !canonicalBrokerDecimal(value, false) {
			return errors.New("broker snapshot totals contain an invalid decimal")
		}
	}
	for _, value := range []string{snapshot.Totals.PurchaseAmount, snapshot.Totals.EvaluationAmount, snapshot.Totals.LoanAmount, snapshot.Totals.CreditLoanAmount, snapshot.Totals.CreditLendingAmount} {
		if !canonicalBrokerDecimal(value, true) {
			return errors.New("broker snapshot totals contain an invalid non-negative decimal")
		}
	}
	previousSymbol := ""
	for _, position := range snapshot.Positions {
		if !kiwoomStockPattern.MatchString(position.Symbol) || position.Symbol <= previousSymbol {
			return errors.New("broker positions must have unique ascending KRX symbols")
		}
		if text, ok := kiwoomText(position.Name); !ok || text != position.Name {
			return errors.New("broker position name is invalid")
		}
		for _, value := range []string{position.Quantity, position.TradableQuantity, position.AveragePurchasePrice, position.CurrentPrice, position.PurchaseAmount, position.EvaluationAmount, position.WeightPercent} {
			if !canonicalBrokerDecimal(value, true) {
				return errors.New("broker position contains an invalid non-negative decimal")
			}
		}
		for _, value := range []string{position.UnrealizedPnL, position.ReturnRatePercent} {
			if !canonicalBrokerDecimal(value, false) {
				return errors.New("broker position contains an invalid signed decimal")
			}
		}
		quantity, _ := exact.ParseDecimal(position.Quantity)
		tradable, _ := exact.ParseDecimal(position.TradableQuantity)
		if tradable.Cmp(quantity) > 0 {
			return errors.New("broker tradable quantity exceeds position quantity")
		}
		previousSymbol = position.Symbol
	}
	previousOrder := ""
	seenOrder := map[string]bool{}
	for _, order := range snapshot.OpenOrders {
		key := order.Symbol + "\x00" + order.Time + "\x00" + order.OrderRef
		if !orderAlias(order.OrderRef, "order") || seenOrder[order.OrderRef] || key <= previousOrder ||
			!kiwoomStockPattern.MatchString(order.Symbol) || order.Status != "OPEN" ||
			(order.Side != "BUY" && order.Side != "SELL") || order.Exchange != string(BrokerKRX) || !kiwoomValidTime(order.Time) {
			return errors.New("broker open order is invalid or unsorted")
		}
		if text, ok := kiwoomText(order.Name); !ok || text != order.Name {
			return errors.New("broker open order name is invalid")
		}
		for _, value := range []string{order.Quantity, order.Price, order.RemainingQuantity} {
			if !canonicalBrokerDecimal(value, true) {
				return errors.New("broker open order contains an invalid decimal")
			}
		}
		quantity, _ := exact.ParseDecimal(order.Quantity)
		remaining, _ := exact.ParseDecimal(order.RemainingQuantity)
		if remaining.Cmp(quantity) > 0 {
			return errors.New("broker remaining quantity exceeds order quantity")
		}
		seenOrder[order.OrderRef], previousOrder = true, key
	}
	return nil
}

func ValidateBrokerReconciliation(record *BrokerReconciliationRecord, snapshot *BrokerSnapshot) error {
	if record == nil || snapshot == nil || !safeBrokerID(record.ReconciliationID) || !safeBrokerID(record.SnapshotID) ||
		record.LedgerAccountID != "account-main" || !ledgerRevisionPattern.MatchString(record.LedgerRevision) || !CanonicalUTCString(record.RecordedAt) {
		return errors.New("broker reconciliation metadata is invalid")
	}
	if len(record.PositionDifferences) == 0 && len(snapshot.Positions) != 0 {
		return errors.New("broker reconciliation is missing positions")
	}
	allMatch, previous := true, ""
	brokerQuantities := map[string]string{}
	for _, position := range snapshot.Positions {
		brokerQuantities[position.Symbol] = position.Quantity
	}
	for _, difference := range record.PositionDifferences {
		if !kiwoomStockPattern.MatchString(difference.Symbol) || difference.Symbol <= previous ||
			!canonicalBrokerDecimal(difference.BrokerQuantity, true) || !canonicalBrokerDecimal(difference.LedgerQuantity, true) || !canonicalBrokerDecimal(difference.Difference, false) {
			return errors.New("broker reconciliation row is invalid")
		}
		brokerQuantity, _ := exact.ParseDecimal(difference.BrokerQuantity)
		ledgerQuantity, _ := exact.ParseDecimal(difference.LedgerQuantity)
		expected := new(big.Rat).Sub(brokerQuantity, ledgerQuantity)
		actual, _ := exact.ParseDecimal(difference.Difference)
		if expected.Cmp(actual) != 0 || difference.Match != (actual.Sign() == 0) {
			return errors.New("broker reconciliation arithmetic is invalid")
		}
		if raw, ok := brokerQuantities[difference.Symbol]; ok && raw != difference.BrokerQuantity {
			return errors.New("broker reconciliation quantity differs from snapshot")
		}
		if _, ok := brokerQuantities[difference.Symbol]; !ok && difference.BrokerQuantity != "0" {
			return errors.New("broker reconciliation invented a broker position")
		}
		delete(brokerQuantities, difference.Symbol)
		allMatch, previous = allMatch && difference.Match, difference.Symbol
	}
	if len(brokerQuantities) != 0 || allMatch != record.AllPositionsMatch {
		return errors.New("broker reconciliation projection is incomplete")
	}
	return nil
}
