package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"

	"strings"
	"time"
)

const (
	legacyPaperSignalSchema = "paper-signal.v1"
	paperSignalSchema       = "paper-signal.v2"
)

type PaperSignal = contracts.PaperSignal

type PaperMarketObservation = contracts.PaperMarketObservation

func (s *Store) RunPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, observation PaperMarketObservation, fencingToken int64) (*OrderState, error) {
	if s == nil || s.db == nil {
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
	state, err := s.recordPaperTarget(ctx, accountRef, signal, observation.AskPrice, target, generatedAt, expiresAt, now, fencingToken)
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

func validatePaperSignal(signal PaperSignal) (*big.Int, time.Time, time.Time, error) {
	dataAsOf, dataOK := canonicalUTCTime(signal.DataAsOf)
	generatedAt, generatedOK := canonicalUTCTime(signal.GeneratedAt)
	expiresAt, expiresOK := canonicalUTCTime(signal.ExpiresAt)
	if len(signal.TargetQuantity) == 0 || len(signal.TargetQuantity) > 64 {
		return nil, time.Time{}, time.Time{}, errors.New("paper signal is invalid")
	}
	target, targetOK := new(big.Int).SetString(signal.TargetQuantity, 10)
	if signal.SchemaVersion != paperSignalSchema || !safeOrderID(signal.SignalID) || !kiwoomStockPattern.MatchString(signal.Symbol) ||
		!strategySHA256Pattern.MatchString(signal.StrategyResultSHA256) || !safeOrderID(signal.StrategySelectionEventID) ||
		!strategySHA256Pattern.MatchString(signal.DataSHA256) || !targetOK || target.Sign() <= 0 || !validOrderInteger(signal.TargetQuantity) ||
		!dataOK || !generatedOK || !expiresOK || dataAsOf.After(generatedAt) || !generatedAt.Before(expiresAt) ||
		!safeOrderID("paper_"+signal.SignalID) {
		return nil, time.Time{}, time.Time{}, errors.New("paper signal is invalid")
	}
	return target, generatedAt, expiresAt, nil
}

func paperOrderIntent(accountRef string, signal PaperSignal, quantity, limitPrice string) OrderIntent {
	return OrderIntent{
		ClientOrderID: "paper_" + signal.SignalID, Provider: "kiwoom", Mode: "paper", AccountRef: accountRef,
		Symbol: signal.Symbol, Exchange: "KRX", Side: "BUY", OrderType: "LIMIT", Quantity: quantity,
		LimitPrice: limitPrice, Currency: "KRW", StrategyResultSHA256: signal.StrategyResultSHA256,
		StrategySelectionEventID: signal.StrategySelectionEventID, SignalSchemaVersion: signal.SchemaVersion,
		SignalID: signal.SignalID, SignalDataSHA256: signal.DataSHA256, SignalDataAsOf: signal.DataAsOf,
		SignalGeneratedAt: signal.GeneratedAt, SignalExpiresAt: signal.ExpiresAt, SignalTargetQuantity: signal.TargetQuantity,
	}
}

var capitalizedPaperOrderIntent = application.CapitalizedPaperOrderIntent

func (s *Store) AdmitPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
	if s == nil || s.db == nil || s.now == nil || !orderAlias(accountRef, "account") || fencingToken <= 0 {
		return nil, nil, errors.New("paper signal admission is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	event, state, err := s.admitPaperSignalTx(ctx, tx, accountRef, signal, fencingToken)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return event, state, nil
}

func (s *Store) admitPaperSignalTx(ctx context.Context, tx *sql.Tx, accountRef string, signal PaperSignal, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
	return s.business().AdmitPaperSignalTx(ctx, &writeScope{store: s, tx: tx}, accountRef, signal, fencingToken)
}

func (s *Store) RunPaperOrder(ctx context.Context, orderID string, fencingToken int64) (*OrderState, error) {
	return s.runPaperOrderWithClaim(ctx, orderID, fencingToken, nil)
}

func (s *Store) runPaperOrderWithClaim(ctx context.Context, orderID string, fencingToken int64, claim *paperRunnerClaim) (*OrderState, error) {
	return s.runPaperOrderThroughCloseWithClaim(ctx, orderID, fencingToken, claim, "")
}

func (s *Store) runPaperOrderThroughCloseWithClaim(ctx context.Context, orderID string, fencingToken int64, claim *paperRunnerClaim, throughClose string) (*OrderState, error) {
	return s.business().RunPaperOrderThroughCloseWithClaim(ctx, orderID, fencingToken, claim, throughClose)
}

func appendCapitalizedPaperFillTx(ctx context.Context, tx *sql.Tx, event OrderEvent, recordedAt string) (*OrderState, error) {
	if err := validateOrderEvent(event); err != nil {
		return nil, err
	}
	return appendOrderEventTxMode(ctx, tx, event, recordedAt, true)
}

func paperOrderUsedFillBars(ctx context.Context, q orderQuerier, orderID string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT json_extract(event_json, '$.paper_bar_observation_id') FROM order_events
		WHERE order_id=? AND event_type='FILL_RECORDED' AND json_extract(event_json, '$.paper_fill_policy_version')=?`, orderID, paperFillPolicyVersion)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for rows.Next() {
		var observationID string
		if err := rows.Scan(&observationID); err != nil {
			rows.Close()
			return nil, err
		}
		used[observationID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	return used, rows.Close()
}

func paperConsumedBarCapacity(ctx context.Context, q orderQuerier, accountRef, symbol, observationID string) (*big.Int, error) {
	rows, err := q.QueryContext(ctx, `SELECT events.event_json FROM order_events events
		JOIN order_idempotency orders ON orders.order_id=events.order_id
		WHERE events.event_type='FILL_RECORDED' AND orders.mode='paper' AND orders.account_ref=?
		  AND json_extract(orders.intent_json, '$.signal_schema_version')='paper-signal.v3'
		  AND json_extract(orders.intent_json, '$.symbol')=?
		  AND json_extract(events.event_json, '$.paper_bar_observation_id')=?`, accountRef, symbol, observationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	consumed := new(big.Int)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event OrderEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		quantity, ok := new(big.Int).SetString(event.Quantity, 10)
		if !ok || quantity.Sign() <= 0 {
			return nil, errors.New("paper consumed capacity is invalid")
		}
		consumed.Add(consumed, quantity)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return consumed, nil
}

func (s *Store) recordPaperTarget(ctx context.Context, accountRef string, signal PaperSignal, limitPrice string, target *big.Int, generatedAt, expiresAt, now time.Time, fencingToken int64) (*OrderState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, _, err := proveExecutionAuthorityRecovery(ctx, tx); err != nil {
		return nil, fmt.Errorf("execution authority recovery: %w", err)
	}
	orderID, exists, err := paperOrderBySignalFrom(ctx, tx, accountRef, signal.SignalID)
	if err != nil {
		return nil, err
	}
	if exists {
		intent, err := loadOrderIntentFrom(ctx, tx, orderID)
		if err != nil || intent != paperOrderIntent(accountRef, signal, intent.Quantity, intent.LimitPrice) {
			return nil, errors.New("paper signal conflicts with its recorded order")
		}
		return loadOrderStateFrom(ctx, tx, orderID)
	}
	if now.Before(generatedAt) || !now.Before(expiresAt) {
		return nil, errors.New("paper signal is not active")
	}
	projected, err := paperProjectedQuantityFrom(ctx, tx, accountRef, signal.Symbol)
	if err != nil {
		return nil, err
	}
	delta := new(big.Int).Sub(target, projected)
	if delta.Sign() <= 0 {
		return nil, nil
	}
	intent := paperOrderIntent(accountRef, signal, delta.String(), limitPrice)
	if _, _, err := validateSyntheticBuyPolicy(intent); err != nil {
		return nil, err
	}
	state, err := s.recordOrderIntentTx(ctx, tx, intent)
	if err != nil {
		return nil, err
	}
	state, _, err = s.authorizeSyntheticDispatchOnceTx(ctx, tx, state.OrderID, fencingToken)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return state, nil
}

func validatePaperObservation(observation PaperMarketObservation, signal PaperSignal, generatedAt, now time.Time) (time.Time, *big.Rat, *big.Rat, error) {
	observedAt, timeOK := canonicalUTCTime(observation.ObservedAt)
	ask, askErr := parseDecimal(observation.AskPrice)
	available, quantityErr := parseDecimal(observation.AvailableQuantity)
	if observation.Source != "local_fixture" || observation.Symbol != signal.Symbol || !timeOK ||
		!observedAt.After(generatedAt) || observedAt.After(now) || askErr != nil || ask.Sign() <= 0 ||
		quantityErr != nil || available.Sign() <= 0 || !validOrderInteger(observation.AvailableQuantity) {
		return time.Time{}, nil, nil, errors.New("paper market observation is invalid")
	}
	return observedAt, ask, available, nil
}

func paperOrderBySignalFrom(ctx context.Context, q orderQuerier, accountRef, signalID string) (string, bool, error) {
	if !orderAlias(accountRef, "account") || !safeOrderID(signalID) {
		return "", false, errors.New("paper execution identity is invalid")
	}
	var orderID string
	err := q.QueryRowContext(ctx, `SELECT order_id FROM order_idempotency WHERE provider=? AND mode=? AND account_ref=? AND client_order_id=?`,
		"kiwoom", "paper", accountRef, "paper_"+signalID).Scan(&orderID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return orderID, err == nil, err
}

func paperProjectedQuantityFrom(ctx context.Context, q orderQuerier, accountRef, symbol string) (*big.Int, error) {
	projection, err := replayPaperProjectionFrom(ctx, q, accountRef, symbol)
	if err != nil {
		return nil, err
	}
	return projection.Quantity, nil
}

type paperProjection = application.Projection

func replayPaperProjectionFrom(ctx context.Context, q orderQuerier, accountRef, symbol string) (paperProjection, error) {
	rows, err := q.QueryContext(ctx, `SELECT order_id FROM order_idempotency WHERE mode='paper' AND account_ref=? ORDER BY rowid`, accountRef)
	if err != nil {
		return paperProjection{}, err
	}
	var orderIDs []string
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return paperProjection{}, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return paperProjection{}, err
	}
	if err := rows.Close(); err != nil {
		return paperProjection{}, err
	}
	projected := new(big.Int)
	activeCount := 0
	for _, orderID := range orderIDs {
		intent, err := loadOrderIntentFrom(ctx, q, orderID)
		if err != nil {
			return paperProjection{}, err
		}
		if intent.SignalSchemaVersion != capitalizedPaperSignalSchema {
			return paperProjection{}, errors.New("paper projection contains an uncapitalized legacy order")
		}
		if intent.Symbol != symbol {
			continue
		}
		state, err := loadOrderStateFrom(ctx, q, orderID)
		if err != nil {
			return paperProjection{}, err
		}
		total, totalOK := new(big.Int).SetString(state.Quantity, 10)
		filled, filledOK := new(big.Int).SetString(state.FilledQuantity, 10)
		if !totalOK || !filledOK || filled.Sign() < 0 || filled.Cmp(total) > 0 {
			return paperProjection{}, errors.New("paper projected quantity is invalid")
		}
		sign := int64(1)
		if intent.Side == "SELL" {
			sign = -1
		}
		projected.Add(projected, new(big.Int).Mul(filled, big.NewInt(sign)))
		active := state.Status == "RECORDED" || state.Status == "READY" || reservationIsActive(state)
		if active {
			activeCount++
			remaining := new(big.Int).Sub(total, filled)
			projected.Add(projected, remaining.Mul(remaining, big.NewInt(sign)))
		}
	}
	if activeCount > 1 || projected.Sign() < 0 {
		return paperProjection{}, errors.New("paper projection violates the one-active-order or long-only boundary")
	}
	return paperProjection{Quantity: projected, Active: activeCount == 1}, nil
}

func paperProviderAlias(kind string, parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "kiwoom_" + kind + "_" + base64.RawURLEncoding.EncodeToString(hash[:18])
}

func paperEventID(kind string, parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "paper_" + kind + "_" + hex.EncodeToString(hash[:16])
}
