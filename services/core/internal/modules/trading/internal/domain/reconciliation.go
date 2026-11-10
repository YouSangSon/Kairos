package domain

import (
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"sort"
	"time"
)

type OrderLookup struct {
	Provider   string
	Mode       string
	AccountRef string
	ObservedAt string
	Complete   bool
	Orders     []OrderObservation
}
type OrderObservation struct {
	ProviderOrderRef   string
	Symbol             string
	Exchange           string
	Side               string
	OrderType          string
	Currency           string
	Quantity           string
	LimitPrice         string
	RemainingQuantity  string
	SubmittedAt        string
	ExecutionsComplete bool
	Executions         []ExecutionObservation
}
type ExecutionObservation struct {
	ProviderExecutionRef string
	Quantity             string
	Price                string
	OccurredAt           string
}
type ValidatedOrderObservation struct {
	Order      OrderObservation
	Submitted  time.Time
	Incomplete bool
}

func ValidateKiwoomOrderLookup(lookup OrderLookup) (time.Time, []ValidatedOrderObservation, error) {
	if lookup.Provider != "kiwoom" || lookup.Mode != "synthetic" || !orderAlias(lookup.AccountRef, "account") {
		return time.Time{}, nil, errors.New("invalid Kiwoom lookup envelope")
	}
	observedAt, ok := CanonicalUTCTime(lookup.ObservedAt)
	if !ok {
		return time.Time{}, nil, errors.New("invalid lookup observation time")
	}
	orders := make([]ValidatedOrderObservation, 0, len(lookup.Orders))
	seenOrders := make(map[string]struct{}, len(lookup.Orders))
	seenExecutions := make(map[string]struct{})
	for _, order := range lookup.Orders {
		if !orderAlias(order.ProviderOrderRef, "order") {
			return time.Time{}, nil, errors.New("invalid provider order reference")
		}
		if _, duplicate := seenOrders[order.ProviderOrderRef]; duplicate {
			return time.Time{}, nil, errors.New("duplicate provider order observation")
		}
		seenOrders[order.ProviderOrderRef] = struct{}{}
		if !kiwoomStockPattern.MatchString(order.Symbol) || order.Exchange != "KRX" ||
			(order.Side != "BUY" && order.Side != "SELL") || order.OrderType != "LIMIT" || order.Currency != "KRW" ||
			!validOrderInteger(order.Quantity) || !validOrderNonNegativeInteger(order.RemainingQuantity) || !positiveCanonicalDecimal(order.LimitPrice) {
			return time.Time{}, nil, errors.New("invalid provider order observation")
		}
		submittedAt, ok := CanonicalUTCTime(order.SubmittedAt)
		if !ok || submittedAt.After(observedAt) {
			return time.Time{}, nil, errors.New("invalid provider order time")
		}
		fillTotal, _ := exact.ParseDecimal("0")
		for _, execution := range order.Executions {
			if !orderAlias(execution.ProviderExecutionRef, "execution") {
				return time.Time{}, nil, errors.New("invalid provider execution reference")
			}
			if _, duplicate := seenExecutions[execution.ProviderExecutionRef]; duplicate {
				return time.Time{}, nil, errors.New("duplicate provider execution observation")
			}
			seenExecutions[execution.ProviderExecutionRef] = struct{}{}
			occurredAt, ok := CanonicalUTCTime(execution.OccurredAt)
			if !ok || occurredAt.Before(submittedAt) || occurredAt.After(observedAt) ||
				!validOrderInteger(execution.Quantity) || !positiveCanonicalDecimal(execution.Price) {
				return time.Time{}, nil, errors.New("invalid provider execution observation")
			}
			quantity, _ := exact.ParseDecimal(execution.Quantity)
			fillTotal.Add(fillTotal, quantity)
		}
		remaining, _ := exact.ParseDecimal(order.RemainingQuantity)
		total, _ := exact.ParseDecimal(order.Quantity)
		accounted := new(big.Rat).Add(fillTotal, remaining)
		if accounted.Cmp(total) > 0 || (order.ExecutionsComplete && accounted.Cmp(total) != 0) {
			return time.Time{}, nil, errors.New("provider execution quantities conflict")
		}
		incomplete := !order.ExecutionsComplete
		orders = append(orders, ValidatedOrderObservation{Order: order, Submitted: submittedAt, Incomplete: incomplete})
	}
	return observedAt, orders, nil
}

var validateKiwoomOrderLookup = ValidateKiwoomOrderLookup

func ReconciliationEvents(orderID string, state *orderdomain.State, order OrderObservation) ([]orderdomain.Event, error) {
	if len(state.ProviderOrderRefs) != 1 || state.ProviderOrderRefs[0] != order.ProviderOrderRef {
		return nil, errors.New("order state cannot accept lookup reconciliation")
	}
	events := make([]orderdomain.Event, 0, len(order.Executions))
	executions := append([]ExecutionObservation(nil), order.Executions...)
	sort.Slice(executions, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, executions[i].OccurredAt)
		right, _ := time.Parse(time.RFC3339Nano, executions[j].OccurredAt)
		if left.Equal(right) {
			return executions[i].ProviderExecutionRef < executions[j].ProviderExecutionRef
		}
		return left.Before(right)
	})
	for _, execution := range executions {
		events = append(events, orderdomain.Event{
			EventID: reconciliationEventID("fill", orderID, execution.ProviderExecutionRef), OrderID: orderID,
			Type: "FILL_RECORDED", Source: "reconciliation", ProviderOrderRef: order.ProviderOrderRef,
			ProviderExecutionRef: execution.ProviderExecutionRef, Quantity: execution.Quantity, Price: execution.Price,
			OccurredAt: execution.OccurredAt,
		})
	}
	return events, nil
}

var reconciliationEvents = ReconciliationEvents

func PositiveCanonicalDecimal(raw string) bool {
	value, err := exact.ParseDecimal(raw)
	return err == nil && value.Sign() > 0
}

var positiveCanonicalDecimal = PositiveCanonicalDecimal

func ValidOrderNonNegativeInteger(raw string) bool {
	return raw == "0" || validOrderInteger(raw)
}

var validOrderNonNegativeInteger = ValidOrderNonNegativeInteger

func ReconciliationEventID(kind, orderID, ref string) string {
	_, hash, _ := orderJSONHash([]string{kind, orderID, ref})
	return "reconciliation_" + hash[:32]
}

var reconciliationEventID = ReconciliationEventID
