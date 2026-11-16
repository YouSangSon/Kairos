package ledgerdomain

import (
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"sort"
	"time"
)

const ZeroTime = "1970-01-01T00:00:00Z"
const LegacyFIFOCostBasisPolicy = "fifo_exact_else_half_even_residual_8_v1"

type ReplayEvent struct {
	Transaction
	ReceiptID string
}

func Revision(n int64) string { return fmt.Sprintf("rev_%010d", n) }
func CanonicalUTCString(raw string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == raw
}

type lot struct {
	quantity *big.Rat
	cost     *big.Rat
}

type position struct {
	instrument, symbol, currency string
	lots                         []lot
}

func Replay(policy string, rev int64, recorded string, events []ReplayEvent) (*PortfolioSnapshot, error) {
	if policy != FIFOCostBasisPolicy && policy != LegacyFIFOCostBasisPolicy {
		return nil, errors.New("unsupported ledger replay policy")
	}
	if !CanonicalUTCString(recorded) {
		return nil, errors.New("ledger recorded_at is not canonical UTC")
	}
	cash := map[string]*big.Rat{}
	realized := map[string]*big.Rat{}
	positions := map[string]*position{}
	eventIDs := []string{}
	receiptIDs := []string{}
	seenReceipt := map[string]bool{}
	asOf := ZeroTime
	for _, event := range events {
		eventID, typ, occurredAt, instrument, symbol, quantityRaw, priceRaw, feeRaw, currency, amountRaw, counterCurrency, counterAmountRaw, receiptID := event.EventID, event.Type, event.OccurredAt, event.InstrumentID, event.Symbol, event.Quantity, event.Price, event.Fee, event.Currency, event.Amount, event.CounterCurrency, event.CounterAmount, event.ReceiptID
		amount, err := exact.ParseDecimal(amountRaw)
		if err != nil {
			return nil, fmt.Errorf("event %s amount: %w", eventID, err)
		}
		addRat(cash, currency, amount)
		eventIDs = append(eventIDs, eventID)
		if !seenReceipt[receiptID] {
			seenReceipt[receiptID] = true
			receiptIDs = append(receiptIDs, receiptID)
		}
		if !CanonicalUTCString(occurredAt) {
			return nil, errors.New("ledger occurred_at is not canonical UTC")
		}
		if policy == FIFOCostBasisPolicy || occurredAt > asOf {
			asOf = occurredAt
		}
		if typ == "FX_EXCHANGE" {
			counterAmount, err := exact.ParseDecimal(counterAmountRaw)
			if err != nil {
				return nil, fmt.Errorf("event %s counter amount: %w", eventID, err)
			}
			addRat(cash, counterCurrency, counterAmount)
			continue
		}
		switch typ {
		case "DEPOSIT", "WITHDRAWAL", "DIVIDEND", "FEE", "TAX", "CASH_VOID":
			continue
		}
		quantity, err := exact.ParseDecimal(quantityRaw)
		if err != nil {
			return nil, err
		}
		key := instrument + "\x00" + currency
		p := positions[key]
		if typ == "SPLIT" {
			if p == nil || len(p.lots) == 0 {
				return nil, fmt.Errorf("SPLIT %s has no open holding", eventID)
			}
			for i := range p.lots {
				p.lots[i].quantity.Mul(p.lots[i].quantity, quantity)
			}
			continue
		}
		price, err := exact.ParseDecimal(priceRaw)
		if err != nil {
			return nil, err
		}
		fee, err := exact.ParseDecimal(feeRaw)
		if err != nil {
			return nil, err
		}
		if p == nil {
			p = &position{instrument: instrument, symbol: symbol, currency: currency}
			positions[key] = p
		}
		if typ == "BUY" {
			cost := new(big.Rat).Add(new(big.Rat).Mul(quantity, price), fee)
			p.lots = append(p.lots, lot{new(big.Rat).Set(quantity), cost})
			continue
		}
		remaining := new(big.Rat).Set(quantity)
		allocatedCost := new(big.Rat)
		for remaining.Sign() > 0 && len(p.lots) > 0 {
			current := &p.lots[0]
			take := new(big.Rat).Set(remaining)
			if take.Cmp(current.quantity) > 0 {
				take.Set(current.quantity)
			}
			allocation, err := exact.FIFOAllocation(current.cost, take, current.quantity)
			if err != nil {
				return nil, fmt.Errorf("SELL %s cost allocation: %w", eventID, err)
			}
			allocatedCost.Add(allocatedCost, allocation)
			current.quantity.Sub(current.quantity, take)
			current.cost.Sub(current.cost, allocation)
			remaining.Sub(remaining, take)
			if current.quantity.Sign() == 0 {
				p.lots = p.lots[1:]
			}
		}
		if remaining.Sign() != 0 {
			return nil, fmt.Errorf("SELL %s exceeds available FIFO quantity", eventID)
		}
		addRat(realized, currency, new(big.Rat).Sub(amount, allocatedCost))
	}
	snapshot := &PortfolioSnapshot{
		PortfolioID: "portfolio_main", LedgerRevision: Revision(rev), CostBasisPolicy: policy, AsOf: asOf, RecordedAt: recorded,
		LiveEnabled: false, ValuationStatus: "unavailable", Cash: []Money{}, Holdings: []Holding{}, RealizedPnL: []Money{},
		Provenance: Provenance{EventIDs: eventIDs, ReceiptIDs: receiptIDs},
	}
	for _, currency := range sortedKeys(cash) {
		amount, err := exact.FormatDecimal(cash[currency])
		if err != nil {
			return nil, err
		}
		snapshot.Cash = append(snapshot.Cash, Money{Currency: currency, Amount: amount})
	}
	for _, key := range sortedPositionKeys(positions) {
		p := positions[key]
		quantity, cost := new(big.Rat), new(big.Rat)
		for _, lot := range p.lots {
			quantity.Add(quantity, lot.quantity)
			cost.Add(cost, lot.cost)
		}
		if quantity.Sign() == 0 {
			continue
		}
		quantityString, err := exact.FormatDecimal(quantity)
		if err != nil {
			return nil, err
		}
		costString, err := exact.FormatDecimal(cost)
		if err != nil {
			return nil, err
		}
		snapshot.Holdings = append(snapshot.Holdings, Holding{InstrumentID: p.instrument, Symbol: p.symbol, Quantity: quantityString, CostBasis: costString, Currency: p.currency})
	}
	for _, currency := range sortedKeys(realized) {
		amount, err := exact.FormatDecimal(realized[currency])
		if err != nil {
			return nil, err
		}
		snapshot.RealizedPnL = append(snapshot.RealizedPnL, Money{Currency: currency, Amount: amount})
	}
	return snapshot, nil
}

func addRat(values map[string]*big.Rat, key string, value *big.Rat) {
	if values[key] == nil {
		values[key] = new(big.Rat)
	}
	values[key].Add(values[key], value)
}

func sortedKeys(values map[string]*big.Rat) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedPositionKeys(values map[string]*position) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := values[keys[i]], values[keys[j]]
		if a.instrument == b.instrument {
			return a.currency < b.currency
		}
		return a.instrument < b.instrument
	})
	return keys
}
