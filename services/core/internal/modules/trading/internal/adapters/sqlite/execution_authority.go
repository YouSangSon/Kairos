package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	orderdomain "omni-folio/services/core/internal/modules/trading/internal/domain/order"
	"time"
)

const (
	syntheticExecutionLeaseTTL  = 30 * time.Second
	syntheticRiskPolicyVersion  = "credential_free_buy_v1"
	syntheticMaxOrderQuantity   = int64(10)
	syntheticMaxOrderNotional   = int64(1_000_000)
	syntheticMaxAccountNotional = int64(1_000_000)
)

type ExecutionAuthorityState = contracts.ExecutionAuthorityState

type executionAuthorityRecord = application.AuthorityRecord

type executionAuthoritySnapshot = application.AuthoritySnapshot

type riskReservationRecord = application.RiskReservation

func (s *Store) SetSyntheticExecutionArmed(ctx context.Context, accountRef string, armed bool) (*ExecutionAuthorityState, error) {
	return s.business().SetSyntheticExecutionArmed(ctx, accountRef, armed)
}

func (s *Store) AcquireSyntheticExecutionLease(ctx context.Context, accountRef string) (*ExecutionAuthorityState, error) {
	return s.business().AcquireSyntheticExecutionLease(ctx, accountRef)
}

func (s *Store) acquireSyntheticExecutionLeaseTx(ctx context.Context, tx *transaction, accountRef string) (*ExecutionAuthorityState, error) {
	return s.business().AcquireSyntheticExecutionLeaseTx(ctx, &writeScope{store: s, tx: tx}, accountRef)
}

func (s *Store) AuthorizeSyntheticDispatch(ctx context.Context, orderID string, fencingToken int64) (*OrderState, error) {
	state, _, err := s.authorizeSyntheticDispatchOnce(ctx, orderID, fencingToken)
	return state, err
}

func (s *Store) authorizeSyntheticDispatchOnce(ctx context.Context, orderID string, fencingToken int64) (*OrderState, bool, error) {
	return s.business().AuthorizeSyntheticDispatchOnce(ctx, orderID, fencingToken)
}

func (s *Store) authorizeSyntheticDispatchOnceTx(ctx context.Context, tx *transaction, orderID string, fencingToken int64) (*OrderState, bool, error) {
	return s.business().AuthorizeSyntheticDispatchOnceTx(ctx, &writeScope{store: s, tx: tx}, orderID, fencingToken)
}

func (s *Store) requireCurrentSyntheticExecutionLease(ctx context.Context, q orderQuerier, accountRef string, fencingToken int64, now time.Time) (executionAuthoritySnapshot, error) {
	authority, err := loadExecutionAuthoritySnapshot(ctx, q, accountRef)
	if err != nil {
		return executionAuthoritySnapshot{}, err
	}
	if err := s.validateOwnedExecutionLease(authority.ExecutionAuthorityState, fencingToken, now); err != nil {
		return executionAuthoritySnapshot{}, err
	}
	return authority, nil
}

func (s *Store) validateOwnedExecutionLease(authority ExecutionAuthorityState, fencingToken int64, now time.Time) error {
	return s.business().ValidateOwnedExecutionLease(authority, fencingToken, now)
}

func (s *Store) haltAllSyntheticExecutionTx(ctx context.Context, tx *transaction, now time.Time) error {
	accountRefs, err := armedExecutionAccounts(ctx, tx)
	if err != nil {
		return err
	}
	_, err = s.haltSyntheticExecutionTx(ctx, tx, now, "manual_halt", "", accountRefs, nil)
	return err
}

func (s *Store) haltSyntheticExecutionTx(ctx context.Context, tx *transaction, now time.Time, reasonCode, policyEventID string, accountRefs []string, automaticHaltIDs map[string]string) (int, error) {
	return s.business().HaltSyntheticExecutionTx(ctx, &writeScope{store: s, tx: tx}, now, reasonCode, policyEventID, accountRefs, automaticHaltIDs)
}

var validateSyntheticBuyPolicy = application.ValidateSyntheticBuyPolicy

func activeReservedNotional(ctx context.Context, q orderQuerier, accountRef string) (*big.Rat, error) {
	rows, err := q.QueryContext(ctx, `SELECT order_id FROM risk_reservations WHERE account_ref=? ORDER BY sequence`, accountRef)
	if err != nil {
		return nil, err
	}
	var orderIDs []string
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return nil, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	total := new(big.Rat)
	for _, orderID := range orderIDs {
		reservation, found, err := loadRiskReservationByOrder(ctx, q, orderID)
		if err != nil || !found {
			return nil, fmt.Errorf("load risk reservation: %w", err)
		}
		state, err := validateAuthorizedReservation(ctx, q, reservation)
		if err != nil {
			return nil, err
		}
		if reservationIsActive(state) {
			value, _ := parseDecimal(reservation.LimitNotional)
			total.Add(total, value)
		}
	}
	return total, nil
}

func reservationIsActive(state *OrderState) bool {
	return domain.ReservationIsActive((*orderdomain.State)(state))
}

func validateAuthorizedReservation(ctx context.Context, q orderQuerier, reservation riskReservationRecord) (*OrderState, error) {
	intent, err := loadOrderIntentFrom(ctx, q, reservation.OrderID)
	if err != nil {
		return nil, err
	}
	notional, _, err := validateSyntheticBuyPolicy(intent)
	if err != nil || reservation.AccountRef != intent.AccountRef || reservation.Quantity != intent.Quantity ||
		reservation.LimitPrice != intent.LimitPrice || reservation.LimitNotional != notional {
		return nil, errors.New("risk reservation does not match its order intent")
	}
	authority, err := loadExecutionAuthorityRecordByID(ctx, q, reservation.AuthorityEventID)
	if err != nil || authority.AccountRef != reservation.AccountRef || authority.FencingToken != reservation.FencingToken ||
		authority.ReasonCode != "lease_acquired" || authority.LeaseOwner == "" {
		return nil, errors.New("risk reservation does not match its execution authority")
	}
	if _, err := loadExecutionAuthoritySnapshot(ctx, q, reservation.AccountRef); err != nil {
		return nil, err
	}
	riskSequence, risk, err := loadAuthorizedOrderEvent(ctx, q, reservation.RiskEventID)
	if err != nil {
		return nil, err
	}
	dispatchSequence, dispatch, err := loadAuthorizedOrderEvent(ctx, q, reservation.DispatchEventID)
	if err != nil {
		return nil, err
	}
	for _, event := range []OrderEvent{risk, dispatch} {
		if event.OrderID != reservation.OrderID || event.Source != "synthetic" || event.RiskReservationID != reservation.ReservationID ||
			event.RiskPolicyVersion != reservation.PolicyVersion || event.FencingToken != reservation.FencingToken {
			return nil, errors.New("authorized order event does not match its reservation")
		}
	}
	if risk.Type != "RISK_APPROVED" || dispatch.Type != "SUBMIT_DISPATCHED" || dispatchSequence != riskSequence+1 {
		return nil, errors.New("risk reservation is missing its consecutive authorized event pair")
	}
	state, err := loadOrderStateFrom(ctx, q, reservation.OrderID)
	if err != nil {
		return nil, err
	}
	switch state.Status {
	case "SUBMIT_UNKNOWN", "OPEN", "PARTIALLY_FILLED", "CANCEL_UNKNOWN", "REJECTED", "FILLED", "CANCELED":
		return state, nil
	default:
		return nil, errors.New("reserved order is not in an authorized post-dispatch state")
	}
}

var authorizedOrderEvent = application.AuthorizedOrderEvent

var authorityState = application.AuthorityState

func insertExecutionAuthorityRecord(ctx context.Context, tx *transaction, record executionAuthorityRecord) error {
	if err := validateExecutionAuthorityRecordBasic(record); err != nil {
		return err
	}
	raw, hash, err := orderJSONHash(record)
	if err != nil {
		return err
	}
	hasPolicySchema, err := hasPaperPerformancePolicySchema(ctx, tx)
	if err != nil {
		return err
	}
	if !hasPolicySchema {
		_, err = tx.ExecContext(ctx, `INSERT INTO execution_authority_events(event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,record_sha256,record_json,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			record.EventID, record.AccountRef, boolInt(record.Armed), nullable(record.LeaseOwner), record.FencingToken, nullable(record.LeaseExpiresAt),
			record.ReasonCode, hash, string(raw), record.RecordedAt)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO execution_authority_events(event_id,account_ref,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		record.EventID, record.AccountRef, boolInt(record.Armed), nullable(record.LeaseOwner), record.FencingToken, nullable(record.LeaseExpiresAt),
		record.ReasonCode, nullable(record.PaperPerformancePolicyEventID), hash, string(raw), record.RecordedAt)
	return err
}

func loadExecutionAuthoritySnapshot(ctx context.Context, q orderQuerier, accountRef string) (executionAuthoritySnapshot, error) {
	policyColumn, err := executionAuthorityPolicyColumn(ctx, q)
	if err != nil {
		return executionAuthoritySnapshot{}, err
	}
	var rows rowset
	if policyColumn == "NULL" {
		rows, err = q.QueryContext(ctx, `SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,NULL,record_sha256,record_json,recorded_at
		FROM execution_authority_events WHERE account_ref=? ORDER BY sequence`, accountRef)
	} else {
		rows, err = q.QueryContext(ctx, `SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at
		FROM execution_authority_events WHERE account_ref=? ORDER BY sequence`, accountRef)
	}
	if err != nil {
		return executionAuthoritySnapshot{}, err
	}
	defer rows.Close()
	var snapshot executionAuthoritySnapshot
	var previous *executionAuthorityRecord
	for rows.Next() {
		record, err := scanExecutionAuthorityRecord(rows, accountRef)
		if err != nil {
			return executionAuthoritySnapshot{}, err
		}
		if err := validateExecutionAuthorityRecord(record, previous); err != nil {
			return executionAuthoritySnapshot{}, err
		}
		copy := record
		previous = &copy
		snapshot.ExecutionAuthorityState = *authorityState(record)
		snapshot.EventID = record.EventID
		snapshot.Count++
	}
	if err := rows.Err(); err != nil {
		return executionAuthoritySnapshot{}, err
	}
	if snapshot.Count == 0 {
		snapshot.AccountRef = accountRef
	}
	return snapshot, nil
}

type rowScanner interface{ Scan(...any) error }

func scanExecutionAuthorityRecord(row rowScanner, accountRef string) (executionAuthorityRecord, error) {
	var eventID, reason, hash, raw, recordedAt string
	var armed int
	var owner, expires, policyEventID sql.NullString
	var token int64
	if err := row.Scan(&eventID, &armed, &owner, &token, &expires, &reason, &policyEventID, &hash, &raw, &recordedAt); err != nil {
		return executionAuthorityRecord{}, err
	}
	var record executionAuthorityRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return executionAuthorityRecord{}, err
	}
	canonical, actualHash, err := orderJSONHash(record)
	if err != nil {
		return executionAuthorityRecord{}, err
	}
	if string(canonical) != raw || actualHash != hash || record.EventID != eventID || record.AccountRef != accountRef ||
		boolInt(record.Armed) != armed || record.LeaseOwner != owner.String || (record.LeaseOwner != "") != owner.Valid ||
		record.FencingToken != token || record.LeaseExpiresAt != expires.String || (record.LeaseExpiresAt != "") != expires.Valid ||
		record.ReasonCode != reason || record.PaperPerformancePolicyEventID != policyEventID.String ||
		(record.PaperPerformancePolicyEventID != "") != policyEventID.Valid || record.RecordedAt != recordedAt {
		return executionAuthorityRecord{}, errors.New("execution authority record metadata or hash mismatch")
	}
	return record, nil
}

var validateExecutionAuthorityRecord = application.ValidateExecutionAuthorityRecord

var validateExecutionAuthorityRecordBasic = application.ValidateExecutionAuthorityRecordBasic

func loadExecutionAuthorityRecordByID(ctx context.Context, q orderQuerier, eventID string) (executionAuthorityRecord, error) {
	var accountRef string
	if err := q.QueryRowContext(ctx, `SELECT account_ref FROM execution_authority_events WHERE event_id=?`, eventID).Scan(&accountRef); err != nil {
		return executionAuthorityRecord{}, err
	}
	policyColumn, err := executionAuthorityPolicyColumn(ctx, q)
	if err != nil {
		return executionAuthorityRecord{}, err
	}
	var row rowScanner
	if policyColumn == "NULL" {
		row = q.QueryRowContext(ctx, `SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,NULL,record_sha256,record_json,recorded_at
		FROM execution_authority_events WHERE event_id=?`, eventID)
	} else {
		row = q.QueryRowContext(ctx, `SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at
		FROM execution_authority_events WHERE event_id=?`, eventID)
	}
	record, err := scanExecutionAuthorityRecord(row, accountRef)
	if err != nil {
		return executionAuthorityRecord{}, err
	}
	if err := validateExecutionAuthorityRecordStandalone(record); err != nil {
		return executionAuthorityRecord{}, err
	}
	return record, nil
}

var validateExecutionAuthorityRecordStandalone = application.ValidateExecutionAuthorityRecordStandalone

func insertRiskReservation(ctx context.Context, tx *transaction, record riskReservationRecord) error {
	if err := validateRiskReservationRecord(record); err != nil {
		return err
	}
	raw, hash, err := orderJSONHash(record)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO risk_reservations(reservation_id,order_id,account_ref,policy_version,authority_event_id,fencing_token,quantity,limit_price,limit_notional,risk_event_id,dispatch_event_id,record_sha256,record_json,reserved_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.ReservationID, record.OrderID, record.AccountRef, record.PolicyVersion, record.AuthorityEventID, record.FencingToken,
		record.Quantity, record.LimitPrice, record.LimitNotional, record.RiskEventID, record.DispatchEventID, hash, string(raw), record.ReservedAt)
	return err
}

func loadRiskReservationByOrder(ctx context.Context, q orderQuerier, orderID string) (riskReservationRecord, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT reservation_id,account_ref,policy_version,authority_event_id,fencing_token,quantity,limit_price,limit_notional,risk_event_id,dispatch_event_id,record_sha256,record_json,reserved_at
		FROM risk_reservations WHERE order_id=?`, orderID)
	var reservationID, accountRef, policy, authorityEventID, quantity, price, notional, riskID, dispatchID, hash, raw, reservedAt string
	var token int64
	if err := row.Scan(&reservationID, &accountRef, &policy, &authorityEventID, &token, &quantity, &price, &notional, &riskID, &dispatchID, &hash, &raw, &reservedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return riskReservationRecord{}, false, nil
		}
		return riskReservationRecord{}, false, err
	}
	var record riskReservationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return riskReservationRecord{}, false, err
	}
	canonical, actualHash, err := orderJSONHash(record)
	if err != nil {
		return riskReservationRecord{}, false, err
	}
	if string(canonical) != raw || actualHash != hash || record.ReservationID != reservationID || record.OrderID != orderID ||
		record.AccountRef != accountRef || record.PolicyVersion != policy || record.AuthorityEventID != authorityEventID ||
		record.FencingToken != token || record.Quantity != quantity || record.LimitPrice != price || record.LimitNotional != notional ||
		record.RiskEventID != riskID || record.DispatchEventID != dispatchID || record.ReservedAt != reservedAt {
		return riskReservationRecord{}, false, errors.New("risk reservation metadata or hash mismatch")
	}
	if err := validateRiskReservationRecord(record); err != nil {
		return riskReservationRecord{}, false, err
	}
	return record, true, nil
}

func validateRiskReservationRecord(record riskReservationRecord) error {
	return domain.ValidateRiskReservationRecord(record)
}

func loadAuthorizedOrderEvent(ctx context.Context, q orderQuerier, eventID string) (int64, OrderEvent, error) {
	var sequence int64
	var eventSHA, orderID, eventType, source, raw, reservationID string
	if err := q.QueryRowContext(ctx, `SELECT sequence,event_sha256,order_id,event_type,source,event_json,authority_reservation_id FROM order_events WHERE event_id=?`, eventID).
		Scan(&sequence, &eventSHA, &orderID, &eventType, &source, &raw, &reservationID); err != nil {
		return 0, OrderEvent{}, err
	}
	var event OrderEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return 0, OrderEvent{}, err
	}
	canonical, actualSHA, err := orderJSONHash(event)
	if err != nil || string(canonical) != raw || actualSHA != eventSHA || event.EventID != eventID || event.OrderID != orderID ||
		event.Type != eventType || event.Source != source || event.RiskReservationID != reservationID {
		return 0, OrderEvent{}, errors.New("authorized order event metadata mismatch")
	}
	if err := validateOrderEvent(event); err != nil {
		return 0, OrderEvent{}, err
	}
	return sequence, event, nil
}

func proveExecutionAuthorityRecovery(ctx context.Context, q orderQuerier) (string, int, error) {
	policyColumn, err := executionAuthorityPolicyColumn(ctx, q)
	if err != nil {
		return "", 0, err
	}
	var rows rowset
	if policyColumn == "NULL" {
		rows, err = q.QueryContext(ctx, `SELECT sequence,account_ref,event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,NULL,record_sha256,record_json,recorded_at
		FROM execution_authority_events ORDER BY sequence`)
	} else {
		rows, err = q.QueryContext(ctx, `SELECT sequence,account_ref,event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at
		FROM execution_authority_events ORDER BY sequence`)
	}
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	previous := map[string]*executionAuthorityRecord{}
	type policyLink struct{ policyID, eventID, accountRef string }
	var linkedHalts []policyLink
	count := 0
	for rows.Next() {
		var sequence, token int64
		var accountRef, eventID, reason, recordSHA, raw, recordedAt string
		var armed int
		var owner, expires, policyEventID sql.NullString
		if err := rows.Scan(&sequence, &accountRef, &eventID, &armed, &owner, &token, &expires, &reason, &policyEventID, &recordSHA, &raw, &recordedAt); err != nil {
			return "", 0, err
		}
		var record executionAuthorityRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return "", 0, err
		}
		canonical, actualSHA, err := orderJSONHash(record)
		if err != nil || string(canonical) != raw || actualSHA != recordSHA || record.EventID != eventID ||
			record.AccountRef != accountRef || boolInt(record.Armed) != armed || record.LeaseOwner != owner.String ||
			(record.LeaseOwner != "") != owner.Valid || record.FencingToken != token || record.LeaseExpiresAt != expires.String ||
			(record.LeaseExpiresAt != "") != expires.Valid || record.ReasonCode != reason ||
			record.PaperPerformancePolicyEventID != policyEventID.String || (record.PaperPerformancePolicyEventID != "") != policyEventID.Valid || record.RecordedAt != recordedAt {
			return "", 0, errors.New("execution authority recovery metadata or hash mismatch")
		}
		if err := validateExecutionAuthorityRecord(record, previous[accountRef]); err != nil {
			return "", 0, err
		}
		if record.PaperPerformancePolicyEventID != "" {
			linkedHalts = append(linkedHalts, policyLink{policyID: record.PaperPerformancePolicyEventID, eventID: record.EventID, accountRef: record.AccountRef})
		}
		copy := record
		previous[accountRef] = &copy
		hashFields := []any{"execution_authority_events", sequence, accountRef, eventID, armed, owner, token, expires, reason, recordSHA, raw, recordedAt}
		if policyEventID.Valid {
			hashFields = append(hashFields, policyEventID)
		}
		if err := encoder.Encode(hashFields); err != nil {
			return "", 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	if err := rows.Close(); err != nil {
		return "", 0, err
	}
	for _, link := range linkedHalts {
		if err := proveLinkedPaperPerformancePolicyMetadata(ctx, q, link.policyID, "halt", link.eventID, link.accountRef); err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), count, nil
}

func executionAuthorityPolicyColumn(ctx context.Context, q orderQuerier) (string, error) {
	hasPolicySchema, err := hasPaperPerformancePolicySchema(ctx, q)
	if err != nil {
		return "", err
	}
	if hasPolicySchema {
		return "paper_performance_policy_event_id", nil
	}
	return "NULL", nil
}

func hasPaperPerformancePolicySchema(ctx context.Context, q orderQuerier) (bool, error) {
	var schemaVersion int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		return false, err
	}
	return schemaVersion >= 20, nil
}

func proveRiskReservationRecovery(ctx context.Context, q orderQuerier) (string, int, error) {
	rows, err := q.QueryContext(ctx, `SELECT sequence,order_id,reservation_id,account_ref,policy_version,authority_event_id,fencing_token,quantity,limit_price,limit_notional,risk_event_id,dispatch_event_id,record_sha256,record_json,reserved_at
		FROM risk_reservations ORDER BY sequence`)
	if err != nil {
		return "", 0, err
	}
	var orderIDs []string
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	count := 0
	for rows.Next() {
		var sequence, token int64
		var orderID, reservationID, accountRef, policy, authorityID, quantity, price, notional, riskID, dispatchID, recordSHA, raw, reservedAt string
		if err := rows.Scan(&sequence, &orderID, &reservationID, &accountRef, &policy, &authorityID, &token, &quantity, &price, &notional, &riskID, &dispatchID, &recordSHA, &raw, &reservedAt); err != nil {
			rows.Close()
			return "", 0, err
		}
		var record riskReservationRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			rows.Close()
			return "", 0, err
		}
		canonical, actualSHA, err := orderJSONHash(record)
		if err != nil || string(canonical) != raw || actualSHA != recordSHA || record.OrderID != orderID ||
			record.ReservationID != reservationID || record.AccountRef != accountRef || record.PolicyVersion != policy ||
			record.AuthorityEventID != authorityID || record.FencingToken != token || record.Quantity != quantity ||
			record.LimitPrice != price || record.LimitNotional != notional || record.RiskEventID != riskID ||
			record.DispatchEventID != dispatchID || record.ReservedAt != reservedAt {
			rows.Close()
			return "", 0, errors.New("risk reservation recovery metadata or hash mismatch")
		}
		if err := validateRiskReservationRecord(record); err != nil {
			rows.Close()
			return "", 0, err
		}
		if err := encoder.Encode([]any{"risk_reservations", sequence, orderID, reservationID, accountRef, policy, authorityID, token,
			quantity, price, notional, riskID, dispatchID, recordSHA, raw, reservedAt}); err != nil {
			rows.Close()
			return "", 0, err
		}
		orderIDs = append(orderIDs, orderID)
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", 0, err
	}
	if err := rows.Close(); err != nil {
		return "", 0, err
	}
	for _, orderID := range orderIDs {
		record, found, err := loadRiskReservationByOrder(ctx, q, orderID)
		if err != nil || !found {
			return "", 0, fmt.Errorf("load risk reservation recovery row: %w", err)
		}
		if _, err := validateAuthorizedReservation(ctx, q, record); err != nil {
			return "", 0, err
		}
	}
	var taggedEvents, taggedReservations int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(DISTINCT authority_reservation_id) FROM order_events WHERE authority_reservation_id IS NOT NULL`).Scan(&taggedEvents, &taggedReservations); err != nil {
		return "", 0, err
	}
	if taggedEvents != count*2 || taggedReservations != count {
		return "", 0, errors.New("authorized order events do not match durable risk reservations")
	}
	return hex.EncodeToString(hash.Sum(nil)), count, nil
}
