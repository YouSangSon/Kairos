package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

const (
	paperAccountingPolicyVersion      = "paper_accounting_v1"
	paperExecutionAuthorizationSchema = "paper-execution-authorization.v1"
)

type paperExecutionAuthorization = application.PaperAuthorization

func (s *Store) authorizePaperDispatchOnceTx(ctx context.Context, tx *sql.Tx, orderID string, fencingToken int64) (*OrderState, bool, error) {
	return s.business().AuthorizePaperDispatchOnceTx(ctx, &writeScope{store: s, tx: tx}, orderID, fencingToken)
}

func validateCapitalizedPaperOrderBindings(ctx context.Context, q orderQuerier, intent OrderIntent) (*PaperAccountingSession, *PaperSignalEvent, error) {
	if intent.Mode != "paper" || intent.SignalSchemaVersion != capitalizedPaperSignalSchema || intent.OrderType != "PAPER_MARKET" || intent.LimitPrice != "" ||
		intent.PaperAccountingPolicyVersion != paperAccountingPolicyVersion {
		return nil, nil, errors.New("order is not a capitalized paper intent")
	}
	session, found, err := loadPaperAccountingSession(ctx, q, intent.AccountRef)
	if err != nil {
		return nil, nil, err
	}
	if !found || session.SessionID != intent.PaperAccountingSessionID || session.ExecutionPolicySHA256 != intent.ExecutionPolicySHA256 {
		return nil, nil, errors.New("paper order accounting session binding is invalid")
	}
	signal, found, err := loadPaperSignalEvent(ctx, q, intent.AccountRef, intent.SignalID)
	if err != nil {
		return nil, nil, err
	}
	if !found || signal.EventID != intent.PaperSignalEventID || signal.PaperAccountingSessionID != intent.PaperAccountingSessionID ||
		signal.ExecutionPolicySHA256 != intent.ExecutionPolicySHA256 || signal.StrategyResultSHA256 != intent.StrategyResultSHA256 ||
		signal.StrategySelectionEventID != intent.StrategySelectionEventID || signal.DataSHA256 != intent.SignalDataSHA256 ||
		signal.Symbol != intent.Symbol || signal.TargetQuantity != intent.SignalTargetQuantity || signal.DataAsOf != intent.SignalDataAsOf ||
		signal.GeneratedAt != intent.SignalGeneratedAt || signal.ExpiresAt != intent.SignalExpiresAt {
		return nil, nil, errors.New("paper order signal binding is invalid")
	}
	return session, signal, nil
}

var paperAuthorizedOrderEvent = application.PaperAuthorizedOrderEvent

func insertPaperExecutionAuthorization(ctx context.Context, tx *sql.Tx, authorization paperExecutionAuthorization) error {
	if err := validatePaperExecutionAuthorizationRecord(authorization); err != nil {
		return err
	}
	raw, hash, err := orderJSONHash(authorization)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_execution_authorizations(
		authorization_id,schema_version,order_id,account_ref,paper_accounting_session_id,execution_policy_sha256,
		policy_version,side,quantity,authority_event_id,fencing_token,risk_event_id,dispatch_event_id,record_sha256,record_json,authorized_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, authorization.AuthorizationID, authorization.SchemaVersion, authorization.OrderID,
		authorization.AccountRef, authorization.PaperAccountingSessionID, authorization.ExecutionPolicySHA256, authorization.PolicyVersion,
		authorization.Side, authorization.Quantity, authorization.AuthorityEventID, authorization.FencingToken, authorization.RiskEventID,
		authorization.DispatchEventID, hash, string(raw), authorization.AuthorizedAt)
	return err
}

func loadPaperExecutionAuthorizationByOrder(ctx context.Context, q orderQuerier, orderID string) (paperExecutionAuthorization, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT authorization_id,schema_version,account_ref,paper_accounting_session_id,execution_policy_sha256,
		policy_version,side,quantity,authority_event_id,fencing_token,risk_event_id,dispatch_event_id,record_sha256,record_json,authorized_at
		FROM paper_execution_authorizations WHERE order_id=?`, orderID)
	var authorization paperExecutionAuthorization
	var hash, raw string
	authorization.OrderID = orderID
	err := row.Scan(&authorization.AuthorizationID, &authorization.SchemaVersion, &authorization.AccountRef,
		&authorization.PaperAccountingSessionID, &authorization.ExecutionPolicySHA256, &authorization.PolicyVersion,
		&authorization.Side, &authorization.Quantity, &authorization.AuthorityEventID, &authorization.FencingToken,
		&authorization.RiskEventID, &authorization.DispatchEventID, &hash, &raw, &authorization.AuthorizedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return paperExecutionAuthorization{}, false, nil
	}
	if err != nil {
		return paperExecutionAuthorization{}, false, err
	}
	canonical, actualHash, err := orderJSONHash(authorization)
	if err != nil || string(canonical) != raw || actualHash != hash {
		return paperExecutionAuthorization{}, false, errors.New("paper authorization metadata or hash mismatch")
	}
	if err := validatePaperExecutionAuthorizationRecord(authorization); err != nil {
		return paperExecutionAuthorization{}, false, err
	}
	return authorization, true, nil
}

func validatePaperExecutionAuthorizationRecord(authorization paperExecutionAuthorization) error {
	_, timeOK := canonicalUTCTime(authorization.AuthorizedAt)
	if !safeOrderID(authorization.AuthorizationID) || authorization.SchemaVersion != paperExecutionAuthorizationSchema ||
		!safeOrderID(authorization.OrderID) || !orderAlias(authorization.AccountRef, "account") ||
		!safeOrderID(authorization.PaperAccountingSessionID) || !strategySHA256Pattern.MatchString(authorization.ExecutionPolicySHA256) ||
		authorization.PolicyVersion != paperAccountingPolicyVersion || (authorization.Side != "BUY" && authorization.Side != "SELL") ||
		!validOrderInteger(authorization.Quantity) || !safeOrderID(authorization.AuthorityEventID) || authorization.FencingToken <= 0 ||
		!safeOrderID(authorization.RiskEventID) || !safeOrderID(authorization.DispatchEventID) ||
		authorization.RiskEventID == authorization.DispatchEventID || !timeOK {
		return errors.New("paper execution authorization is invalid")
	}
	return nil
}

func validatePaperExecutionAuthorization(ctx context.Context, q orderQuerier, authorization paperExecutionAuthorization) (*OrderState, error) {
	intent, err := loadOrderIntentFrom(ctx, q, authorization.OrderID)
	if err != nil {
		return nil, err
	}
	if _, _, err := validateCapitalizedPaperOrderBindings(ctx, q, intent); err != nil {
		// Retain cancellation identity without exposing a raw storage error.
		return nil, errors.Join(errors.New("paper authorization does not match its order intent"), ctx.Err())
	}
	if authorization.AccountRef != intent.AccountRef || authorization.PaperAccountingSessionID != intent.PaperAccountingSessionID ||
		authorization.ExecutionPolicySHA256 != intent.ExecutionPolicySHA256 || authorization.Side != intent.Side || authorization.Quantity != intent.Quantity {
		return nil, errors.New("paper authorization does not match its order intent")
	}
	authority, err := loadExecutionAuthorityRecordByID(ctx, q, authorization.AuthorityEventID)
	if err != nil {
		return nil, errors.Join(errors.New("paper authorization does not match its execution authority"), ctx.Err())
	}
	if authority.AccountRef != authorization.AccountRef || authority.FencingToken != authorization.FencingToken {
		return nil, errors.New("paper authorization does not match its execution authority")
	}
	authorizedAt, authorizedOK := canonicalUTCTime(authorization.AuthorizedAt)
	authorityRecordedAt, recordedOK := canonicalUTCTime(authority.RecordedAt)
	authorityExpiresAt, expiresOK := canonicalUTCTime(authority.LeaseExpiresAt)
	if !authorizedOK || !recordedOK || !expiresOK || authorizedAt.Before(authorityRecordedAt) || !authorizedAt.Before(authorityExpiresAt) {
		return nil, errors.New("paper authorization is outside its execution lease")
	}
	riskSequence, risk, err := loadPaperAuthorizedOrderEvent(ctx, q, authorization.RiskEventID)
	if err != nil {
		return nil, err
	}
	dispatchSequence, dispatch, err := loadPaperAuthorizedOrderEvent(ctx, q, authorization.DispatchEventID)
	if err != nil {
		return nil, err
	}
	ackSequence, ack, err := loadPaperAuthorizedOrderEvent(ctx, q, paperEventID("ack", authorization.OrderID))
	if err != nil {
		return nil, err
	}
	for _, event := range []OrderEvent{risk, dispatch} {
		if event.OrderID != authorization.OrderID || event.Source != "synthetic" || event.RiskReservationID != "" ||
			event.PaperAuthorizationID != authorization.AuthorizationID || event.RiskPolicyVersion != authorization.PolicyVersion ||
			event.FencingToken != authorization.FencingToken {
			return nil, errors.New("paper authorized event does not match its authorization")
		}
	}
	if risk.Type != "RISK_APPROVED" || dispatch.Type != "SUBMIT_DISPATCHED" || dispatchSequence != riskSequence+1 ||
		ack.Type != "SUBMIT_ACKNOWLEDGED" || ack.Source != "synthetic" || ack.PaperAuthorizationID != "" ||
		ack.ProviderOrderRef != paperProviderAlias("order", authorization.OrderID) || ackSequence != dispatchSequence+1 {
		return nil, errors.New("paper authorization is missing its consecutive local event sequence")
	}
	state, err := loadOrderStateFrom(ctx, q, authorization.OrderID)
	if err != nil {
		return nil, err
	}
	switch state.Status {
	case "OPEN", "PARTIALLY_FILLED", "CANCEL_UNKNOWN", "FILLED", "CANCELED":
		return state, nil
	default:
		return nil, errors.New("paper order is not in an authorized post-acknowledgement state")
	}
}

func loadPaperAuthorizedOrderEvent(ctx context.Context, q orderQuerier, eventID string) (int64, OrderEvent, error) {
	var sequence int64
	var eventSHA, orderID, eventType, source, raw string
	var reservationID, authorizationID sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT sequence,event_sha256,order_id,event_type,source,event_json,authority_reservation_id,paper_authorization_id
		FROM order_events WHERE event_id=?`, eventID).Scan(&sequence, &eventSHA, &orderID, &eventType, &source, &raw, &reservationID, &authorizationID); err != nil {
		return 0, OrderEvent{}, err
	}
	var event OrderEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return 0, OrderEvent{}, err
	}
	canonical, actualSHA, err := orderJSONHash(event)
	if err != nil || string(canonical) != raw || actualSHA != eventSHA || event.EventID != eventID || event.OrderID != orderID ||
		event.Type != eventType || event.Source != source || event.RiskReservationID != reservationID.String ||
		(event.RiskReservationID != "") != reservationID.Valid || event.PaperAuthorizationID != authorizationID.String ||
		(event.PaperAuthorizationID != "") != authorizationID.Valid {
		return 0, OrderEvent{}, errors.New("paper authorized order event metadata mismatch")
	}
	if err := validateOrderEvent(event); err != nil {
		return 0, OrderEvent{}, err
	}
	return sequence, event, nil
}

func provePaperExecutionAuthorizationRecovery(ctx context.Context, q orderQuerier) (string, int, error) {
	rows, err := q.QueryContext(ctx, `SELECT sequence,order_id FROM paper_execution_authorizations ORDER BY sequence`)
	if err != nil {
		return "", 0, err
	}
	type authorizationRow struct {
		sequence int64
		orderID  string
	}
	var stored []authorizationRow
	for rows.Next() {
		var row authorizationRow
		if err := rows.Scan(&row.sequence, &row.orderID); err != nil {
			rows.Close()
			return "", 0, err
		}
		stored = append(stored, row)
	}
	if err := closeRows(rows); err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	for index, row := range stored {
		if row.sequence != int64(index+1) {
			return "", 0, fmt.Errorf("paper authorization sequence %d is invalid", row.sequence)
		}
		authorization, found, err := loadPaperExecutionAuthorizationByOrder(ctx, q, row.orderID)
		if err != nil || !found {
			return "", 0, fmt.Errorf("load paper authorization: %w", err)
		}
		if _, err := validatePaperExecutionAuthorization(ctx, q, authorization); err != nil {
			return "", 0, err
		}
		if err := encoder.Encode([]any{"paper_execution_authorizations", row.sequence, authorization}); err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), len(stored), nil
}
