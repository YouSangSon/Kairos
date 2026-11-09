package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"regexp"
	"strings"

	"unicode"
	"unicode/utf8"
)

var (
	orderIntegerPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
	orderAliasPattern   = regexp.MustCompile(`^kiwoom_(account|order|execution)_[A-Za-z0-9_-]{24}$`)
)

type OrderIntent = contracts.OrderIntent

type OrderEvent = contracts.OrderEvent

type OrderState = contracts.OrderState

type orderQuerier interface {
	QueryContext(context.Context, string, ...any) (rowset, error)
	QueryRowContext(context.Context, string, ...any) rowScanner
}

type orderRecoveryProof struct {
	SHA256                   string
	Orders                   int
	Events                   int
	ExecutionAuthoritySHA256 string
	ExecutionAuthorityEvents int
	RiskReservationSHA256    string
	RiskReservations         int
}

func proveOrderRecovery(ctx context.Context, q orderQuerier) (orderRecoveryProof, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	orderRows, err := q.QueryContext(ctx, `SELECT provider,mode,account_ref,client_order_id,request_sha256,order_id,intent_json,recorded_at
		FROM order_idempotency ORDER BY provider,mode,account_ref,client_order_id`)
	if err != nil {
		return orderRecoveryProof{}, err
	}
	var orderIDs []string
	knownOrders := map[string]bool{}
	for orderRows.Next() {
		var provider, mode, accountRef, clientOrderID, requestSHA, orderID, intentJSON, recordedAt string
		if err := orderRows.Scan(&provider, &mode, &accountRef, &clientOrderID, &requestSHA, &orderID, &intentJSON, &recordedAt); err != nil {
			orderRows.Close()
			return orderRecoveryProof{}, err
		}
		var intent OrderIntent
		if err := json.Unmarshal([]byte(intentJSON), &intent); err != nil {
			orderRows.Close()
			return orderRecoveryProof{}, fmt.Errorf("decode order %q intent: %w", orderID, err)
		}
		canonical, actualSHA, err := orderJSONHash(intent)
		if err != nil {
			orderRows.Close()
			return orderRecoveryProof{}, err
		}
		if string(canonical) != intentJSON || actualSHA != requestSHA || intent.Provider != provider || intent.Mode != mode ||
			intent.AccountRef != accountRef || intent.ClientOrderID != clientOrderID || !safeOrderID(orderID) {
			orderRows.Close()
			return orderRecoveryProof{}, fmt.Errorf("order %q intent metadata or hash mismatch", orderID)
		}
		if err := encoder.Encode([]any{"order_idempotency", provider, mode, accountRef, clientOrderID, requestSHA, orderID, intentJSON, recordedAt}); err != nil {
			orderRows.Close()
			return orderRecoveryProof{}, err
		}
		orderIDs = append(orderIDs, orderID)
		knownOrders[orderID] = true
	}
	if err := orderRows.Err(); err != nil {
		orderRows.Close()
		return orderRecoveryProof{}, err
	}
	if err := orderRows.Close(); err != nil {
		return orderRecoveryProof{}, err
	}

	var hasPaperAuthorizationColumn int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('order_events') WHERE name='paper_authorization_id'`).Scan(&hasPaperAuthorizationColumn); err != nil {
		return orderRecoveryProof{}, err
	}
	var includePaperAuthorizationProof int
	if hasPaperAuthorizationColumn != 0 {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_events WHERE paper_authorization_id IS NOT NULL`).Scan(&includePaperAuthorizationProof); err != nil {
			return orderRecoveryProof{}, err
		}
	}
	eventQuery := `SELECT sequence,event_id,event_sha256,order_id,event_type,source,provider_order_ref,provider_execution_ref,event_json,recorded_at,
		authority_reservation_id FROM order_events ORDER BY sequence`
	if hasPaperAuthorizationColumn != 0 {
		eventQuery = `SELECT sequence,event_id,event_sha256,order_id,event_type,source,provider_order_ref,provider_execution_ref,event_json,recorded_at,
			authority_reservation_id,paper_authorization_id FROM order_events ORDER BY sequence`
	}
	eventRows, err := q.QueryContext(ctx, eventQuery)
	if err != nil {
		return orderRecoveryProof{}, err
	}
	eventCount := 0
	for eventRows.Next() {
		var sequence int64
		var eventID, eventSHA, orderID, eventType, source, eventJSON, recordedAt string
		var providerOrderRef, providerExecutionRef, authorityReservationID, paperAuthorizationID sql.NullString
		values := []any{&sequence, &eventID, &eventSHA, &orderID, &eventType, &source, &providerOrderRef, &providerExecutionRef, &eventJSON, &recordedAt, &authorityReservationID}
		if hasPaperAuthorizationColumn != 0 {
			values = append(values, &paperAuthorizationID)
		}
		if err := eventRows.Scan(values...); err != nil {
			eventRows.Close()
			return orderRecoveryProof{}, err
		}
		if !knownOrders[orderID] {
			eventRows.Close()
			return orderRecoveryProof{}, fmt.Errorf("order event %q references unknown order %q", eventID, orderID)
		}
		var event OrderEvent
		if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
			eventRows.Close()
			return orderRecoveryProof{}, fmt.Errorf("decode order event %q: %w", eventID, err)
		}
		canonical, actualSHA, err := orderJSONHash(event)
		if err != nil {
			eventRows.Close()
			return orderRecoveryProof{}, err
		}
		nullableMatches := func(column sql.NullString, value string) bool {
			return column.Valid == (value != "") && (!column.Valid || column.String == value)
		}
		if string(canonical) != eventJSON || actualSHA != eventSHA || event.EventID != eventID || event.OrderID != orderID ||
			event.Type != eventType || event.Source != source || !nullableMatches(providerOrderRef, event.ProviderOrderRef) ||
			!nullableMatches(providerExecutionRef, event.ProviderExecutionRef) || !nullableMatches(authorityReservationID, event.RiskReservationID) ||
			!nullableMatches(paperAuthorizationID, event.PaperAuthorizationID) {
			eventRows.Close()
			return orderRecoveryProof{}, fmt.Errorf("order event %q metadata or hash mismatch", eventID)
		}
		proofRow := []any{"order_events", sequence, eventID, eventSHA, orderID, eventType, source,
			providerOrderRef, providerExecutionRef, eventJSON, recordedAt, authorityReservationID}
		if includePaperAuthorizationProof != 0 {
			proofRow = append(proofRow, paperAuthorizationID)
		}
		if err := encoder.Encode(proofRow); err != nil {
			eventRows.Close()
			return orderRecoveryProof{}, err
		}
		eventCount++
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return orderRecoveryProof{}, err
	}
	if err := eventRows.Close(); err != nil {
		return orderRecoveryProof{}, err
	}
	for _, orderID := range orderIDs {
		if _, err := loadOrderStateFrom(ctx, q, orderID); err != nil {
			return orderRecoveryProof{}, fmt.Errorf("replay order %q: %w", orderID, err)
		}
	}
	authoritySHA, authorityEvents, err := proveExecutionAuthorityRecovery(ctx, q)
	if err != nil {
		return orderRecoveryProof{}, fmt.Errorf("execution authority recovery: %w", err)
	}
	reservationSHA, reservations, err := proveRiskReservationRecovery(ctx, q)
	if err != nil {
		return orderRecoveryProof{}, fmt.Errorf("risk reservation recovery: %w", err)
	}
	if err := encoder.Encode([]any{"execution_authority", authoritySHA, authorityEvents, "risk_reservations", reservationSHA, reservations}); err != nil {
		return orderRecoveryProof{}, err
	}
	return orderRecoveryProof{
		SHA256: hex.EncodeToString(hash.Sum(nil)), Orders: len(orderIDs), Events: eventCount,
		ExecutionAuthoritySHA256: authoritySHA, ExecutionAuthorityEvents: authorityEvents,
		RiskReservationSHA256: reservationSHA, RiskReservations: reservations,
	}, nil
}

func (s *Store) RecordOrderIntent(ctx context.Context, intent OrderIntent) (*OrderState, error) {
	return s.business().RecordOrderIntent(ctx, intent)
}

func (s *Store) recordOrderIntentTx(ctx context.Context, tx *transaction, intent OrderIntent) (*OrderState, error) {
	return s.business().RecordOrderIntentTx(ctx, &writeScope{store: s, tx: tx}, intent)
}

func (s *Store) AppendOrderEvent(ctx context.Context, event OrderEvent) (*OrderState, error) {
	return s.business().AppendOrderEvent(ctx, event)
}

func appendOrderEventTx(ctx context.Context, tx *transaction, event OrderEvent, recordedAt string) (*OrderState, error) {
	return appendOrderEventTxMode(ctx, tx, event, recordedAt, false)
}

func appendOrderEventTxMode(ctx context.Context, tx *transaction, event OrderEvent, recordedAt string, allowCapitalizedPaperFill bool) (*OrderState, error) {
	if event.Type == "FILL_RECORDED" {
		intent, err := loadOrderIntentFrom(ctx, tx, event.OrderID)
		if err != nil {
			return nil, err
		}
		capitalized := intent.Mode == "paper" && intent.SignalSchemaVersion == capitalizedPaperSignalSchema
		if capitalized && !allowCapitalizedPaperFill {
			return nil, errors.New("capitalized paper fills require the dedicated accounting writer")
		}
		if allowCapitalizedPaperFill && !capitalized {
			return nil, errors.New("dedicated paper fill writer requires a capitalized paper order")
		}
	}
	eventJSON, eventSHA, err := orderJSONHash(event)
	if err != nil {
		return nil, err
	}
	var priorSHA string
	err = tx.QueryRowContext(ctx, `SELECT event_sha256 FROM order_events WHERE event_id=?`, event.EventID).Scan(&priorSHA)
	if err == nil {
		if priorSHA != eventSHA {
			return nil, errors.New("event_id was already used with a different event")
		}
		return loadOrderStateFrom(ctx, tx, event.OrderID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if event.ProviderExecutionRef != "" {
		var priorJSON string
		err := tx.QueryRowContext(ctx, `SELECT event_json FROM order_events WHERE provider_execution_ref=?`, event.ProviderExecutionRef).Scan(&priorJSON)
		if err == nil {
			var prior OrderEvent
			if err := json.Unmarshal([]byte(priorJSON), &prior); err != nil {
				return nil, err
			}
			if !sameProviderExecution(prior, event) {
				return nil, errors.New("provider_execution_ref was already used with a different fill")
			}
			return loadOrderStateFrom(ctx, tx, prior.OrderID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	state, err := loadOrderStateFrom(ctx, tx, event.OrderID)
	if err != nil {
		return nil, err
	}
	if event.Type == "SUBMIT_DISPATCHED" {
		blocked, err := accountHasUnresolvedOrder(ctx, tx, state.AccountRef)
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, errors.New("account has an unresolved order command")
		}
	}
	if event.ProviderOrderRef != "" {
		var boundOrderID string
		err := tx.QueryRowContext(ctx, `SELECT order_id FROM order_events WHERE provider_order_ref=? LIMIT 1`, event.ProviderOrderRef).Scan(&boundOrderID)
		if err == nil && boundOrderID != event.OrderID {
			return nil, errors.New("provider_order_ref is already bound to another order")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	next, err := applyOrderEvent(state, event)
	if err != nil {
		return nil, err
	}
	if err := insertOrderEventRow(ctx, tx, event, eventSHA, string(eventJSON), recordedAt); err != nil {
		return nil, err
	}
	return next, nil
}

func (s *Store) LoadOrderState(ctx context.Context, orderID string) (*OrderState, error) {
	return loadOrderStateFrom(ctx, s.db, orderID)
}

func loadOrderStateFrom(ctx context.Context, q orderQuerier, orderID string) (*OrderState, error) {
	intent, err := loadOrderIntentFrom(ctx, q, orderID)
	if err != nil {
		return nil, err
	}
	state := newOrderState(orderID, intent)
	rows, err := q.QueryContext(ctx, `SELECT event_json FROM order_events WHERE order_id=? ORDER BY sequence`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event OrderEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		if event.OrderID != orderID {
			return nil, errors.New("stored order event belongs to a different order")
		}
		if err := validateOrderEvent(event); err != nil {
			return nil, fmt.Errorf("stored order event is invalid: %w", err)
		}
		state, err = applyOrderEvent(state, event)
		if err != nil {
			return nil, fmt.Errorf("invalid stored order event sequence: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count == 0 || state.Status == "" {
		return nil, errors.New("order has no recorded intent event")
	}
	return state, nil
}

func loadOrderIntentFrom(ctx context.Context, q orderQuerier, orderID string) (OrderIntent, error) {
	var intentJSON string
	if err := q.QueryRowContext(ctx, `SELECT intent_json FROM order_idempotency WHERE order_id=?`, orderID).Scan(&intentJSON); err != nil {
		return OrderIntent{}, err
	}
	var intent OrderIntent
	if err := json.Unmarshal([]byte(intentJSON), &intent); err != nil {
		return OrderIntent{}, err
	}
	if err := validateOrderIntent(intent); err != nil {
		return OrderIntent{}, fmt.Errorf("stored order intent is invalid: %w", err)
	}
	return intent, nil
}

var newOrderState = application.NewOrderState
var applyOrderEvent = application.ApplyOrderEvent

func validateOrderIntent(intent OrderIntent) error {
	return domain.ValidateOrderIntent(domain.OrderIntent(intent))
}

var validateOrderEvent = application.ValidateOrderEvent

func validOrderInteger(raw string) bool {
	return len(raw) <= 32 && orderIntegerPattern.MatchString(raw)
}

func orderAlias(raw, kind string) bool {
	return orderAliasPattern.MatchString(raw) && strings.HasPrefix(raw, "kiwoom_"+kind+"_")
}

func safeOrderID(raw string) bool {
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

func accountHasUnresolvedOrder(ctx context.Context, q orderQuerier, accountRef string) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT order_id FROM order_idempotency WHERE account_ref=? ORDER BY order_id`, accountRef)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var orderIDs []string
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			return false, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	for _, orderID := range orderIDs {
		state, err := loadOrderStateFrom(ctx, q, orderID)
		if err != nil {
			return false, err
		}
		if state.PendingAction != "" {
			return true, nil
		}
	}
	return false, nil
}

func sameProviderExecution(left, right OrderEvent) bool {
	return orderdomain.SameProviderExecution(orderdomain.Event(left), orderdomain.Event(right))
}

var orderJSONHash = codec.JSONHash

func insertOrderEvent(ctx context.Context, tx *transaction, event OrderEvent, recordedAt string) error {
	eventJSON, eventSHA, err := orderJSONHash(event)
	if err != nil {
		return err
	}
	return insertOrderEventRow(ctx, tx, event, eventSHA, string(eventJSON), recordedAt)
}

func insertOrderEventRow(ctx context.Context, tx *transaction, event OrderEvent, eventSHA, eventJSON, recordedAt string) error {
	if err := requirePaperPromotionAppendTime(ctx, tx, recordedAt); err != nil {
		return err
	}
	var hasPaperAuthorizationColumn int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('order_events') WHERE name='paper_authorization_id'`).Scan(&hasPaperAuthorizationColumn); err != nil {
		return err
	}
	if hasPaperAuthorizationColumn == 0 {
		if event.PaperAuthorizationID != "" {
			return errors.New("paper authorization storage is unavailable")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO order_events(event_id,event_sha256,order_id,event_type,source,provider_order_ref,provider_execution_ref,event_json,recorded_at,authority_reservation_id) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			event.EventID, eventSHA, event.OrderID, event.Type, event.Source, nullable(event.ProviderOrderRef), nullable(event.ProviderExecutionRef), eventJSON, recordedAt, nullable(event.RiskReservationID))
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO order_events(event_id,event_sha256,order_id,event_type,source,provider_order_ref,provider_execution_ref,event_json,recorded_at,authority_reservation_id,paper_authorization_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		event.EventID, eventSHA, event.OrderID, event.Type, event.Source, nullable(event.ProviderOrderRef), nullable(event.ProviderExecutionRef), eventJSON, recordedAt, nullable(event.RiskReservationID), nullable(event.PaperAuthorizationID))
	return err
}
