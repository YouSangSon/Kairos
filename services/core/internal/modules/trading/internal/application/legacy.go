package application

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"time"
)

const paperSignalSchema = "paper-signal.v2"

func (s *UseCases) RunPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, observation PaperMarketObservation, fencingToken int64) (*OrderState, error) {
	if s == nil || s.journal == nil {
		return nil, errors.New("paper runner is not configured")
	}
	target, generatedAt, expiresAt, err := validatePaperSignal(signal)
	if err != nil {
		return nil, err
	}
	observedAt, ask, available, err := validatePaperObservation(observation, signal, generatedAt, s.now().UTC())
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	state, err := s.RecordPaperTarget(ctx, accountRef, signal, observation.AskPrice, target, generatedAt, expiresAt, now, fencingToken)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	if state.Status == "RECORDED" {
		if now.Before(generatedAt) || !now.Before(expiresAt) {
			return state, errors.New("paper signal expired before dispatch")
		}
		state, err = s.AuthorizeSyntheticDispatch(ctx, state.OrderID, fencingToken)
		if err != nil {
			return state, err
		}
	}
	orderRef := paperProviderAlias("order", state.OrderID)
	if state.Status == "SUBMIT_UNKNOWN" && state.PendingAction == "SUBMIT" {
		state, err = s.AppendOrderEvent(ctx, OrderEvent{
			EventID: paperEventID("ack", state.OrderID), OrderID: state.OrderID,
			Type: "SUBMIT_ACKNOWLEDGED", Source: "synthetic", ProviderOrderRef: orderRef,
		})
		if err != nil {
			return state, err
		}
	}
	if state.Status != "OPEN" && state.Status != "PARTIALLY_FILLED" {
		return state, nil
	}
	limit, _ := parseDecimal(state.LimitPrice)
	if ask.Cmp(limit) > 0 {
		return state, nil
	}
	total, _ := parseDecimal(state.Quantity)
	filled, _ := parseDecimal(state.FilledQuantity)
	remaining := new(big.Rat).Sub(total, filled)
	fillQuantity := new(big.Rat).Set(available)
	if fillQuantity.Cmp(remaining) > 0 {
		fillQuantity.Set(remaining)
	}
	quantity, err := formatDecimal(fillQuantity)
	if err != nil {
		return state, err
	}
	return s.AppendOrderEvent(ctx, OrderEvent{
		EventID: paperEventID("fill", state.OrderID, observation.ObservedAt, observation.AskPrice, observation.AvailableQuantity),
		OrderID: state.OrderID, Type: "FILL_RECORDED", Source: "synthetic", ProviderOrderRef: orderRef,
		ProviderExecutionRef: paperProviderAlias("execution", state.OrderID, observation.ObservedAt, observation.AskPrice, observation.AvailableQuantity),
		Quantity:             quantity, Price: observation.AskPrice, OccurredAt: observedAt.Format(time.RFC3339Nano),
	})
}
func (s *UseCases) RecordPaperTarget(ctx context.Context, accountRef string, signal PaperSignal, limitPrice string, target *big.Int, generatedAt, expiresAt, now time.Time, fencingToken int64) (*OrderState, error) {
	return decideResult(ctx, s.journal, func(tx WriteScope) (*OrderState, bool, error) {
		if _, _, err := tx.Recovery().ProveAuthorityRecovery(ctx); err != nil {
			return nil, false, fmt.Errorf("execution authority recovery: %w", err)
		}
		orderID, exists, err := tx.Paper().PaperOrderBySignalFrom(ctx, accountRef, signal.SignalID)
		if err != nil {
			return nil, false, err
		}
		if exists {
			intent, err := tx.Orders().LoadOrderIntentFrom(ctx, orderID)
			if err != nil || intent != PaperOrderIntent(accountRef, signal, intent.Quantity, intent.LimitPrice) {
				return nil, false, errors.New("paper signal conflicts with its recorded order")
			}
			state, err := tx.Orders().LoadOrderStateFrom(ctx, orderID)
			return state, false, err
		}
		if now.Before(generatedAt) || !now.Before(expiresAt) {
			return nil, false, errors.New("paper signal is not active")
		}
		projection, err := tx.Paper().ReplayPaperProjectionFrom(ctx, accountRef, signal.Symbol)
		if err != nil {
			return nil, false, err
		}
		delta := new(big.Int).Sub(target, projection.Quantity)
		if delta.Sign() <= 0 {
			return nil, false, nil
		}
		intent := PaperOrderIntent(accountRef, signal, delta.String(), limitPrice)
		if _, _, err := ValidateSyntheticBuyPolicy(intent); err != nil {
			return nil, false, err
		}
		state, err := s.RecordOrderIntentTx(ctx, tx, intent)
		if err != nil {
			return nil, false, err
		}
		state, _, err = s.AuthorizeSyntheticDispatchOnceTx(ctx, tx, state.OrderID, fencingToken)
		if err != nil {
			return nil, false, err
		}
		return state, true, nil
	})
}
func PaperOrderIntent(accountRef string, signal PaperSignal, quantity, limitPrice string) OrderIntent {
	return OrderIntent{
		ClientOrderID: "paper_" + signal.SignalID, Provider: "kiwoom", Mode: "paper", AccountRef: accountRef,
		Symbol: signal.Symbol, Exchange: "KRX", Side: "BUY", OrderType: "LIMIT", Quantity: quantity,
		LimitPrice: limitPrice, Currency: "KRW", StrategyResultSHA256: signal.StrategyResultSHA256,
		StrategySelectionEventID: signal.StrategySelectionEventID, SignalSchemaVersion: signal.SchemaVersion,
		SignalID: signal.SignalID, SignalDataSHA256: signal.DataSHA256, SignalDataAsOf: signal.DataAsOf,
		SignalGeneratedAt: signal.GeneratedAt, SignalExpiresAt: signal.ExpiresAt, SignalTargetQuantity: signal.TargetQuantity,
	}
}
func ValidatePaperSignal(signal PaperSignal) (*big.Int, time.Time, time.Time, error) {
	return domain.ValidatePaperSignal(domain.PaperSignal(signal))
}

var validatePaperSignal = ValidatePaperSignal

func ValidatePaperObservation(observation PaperMarketObservation, signal PaperSignal, generatedAt, now time.Time) (time.Time, *big.Rat, *big.Rat, error) {
	return domain.ValidatePaperObservation(domain.PaperMarketObservation(observation), domain.PaperSignal(signal), generatedAt, now)
}

var validatePaperObservation = ValidatePaperObservation
