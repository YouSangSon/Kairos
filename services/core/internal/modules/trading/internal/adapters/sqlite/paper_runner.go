package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"

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
	return s.business().RunPaperSignal(ctx, accountRef, signal, observation, fencingToken)
}

var validatePaperSignal = application.ValidatePaperSignal

var paperOrderIntent = application.PaperOrderIntent

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

func (s *Store) admitPaperSignalTx(ctx context.Context, tx *transaction, accountRef string, signal PaperSignal, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
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

func appendCapitalizedPaperFillTx(ctx context.Context, tx *transaction, event OrderEvent, recordedAt string) (*OrderState, error) {
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
	if s == nil || s.db == nil {
		return nil, errors.New("paper runner is not configured")
	}
	return s.business().RecordPaperTarget(ctx, accountRef, signal, limitPrice, target, generatedAt, expiresAt, now, fencingToken)
}

var validatePaperObservation = application.ValidatePaperObservation

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
		activeCount, err = domain.AccumulatePaperProjection(projected, activeCount, domain.OrderIntent(intent), (*orderdomain.State)(state))
		if err != nil {
			return paperProjection{}, err
		}
	}
	if err := domain.ValidatePaperProjection(projected, activeCount); err != nil {
		return paperProjection{}, err
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
