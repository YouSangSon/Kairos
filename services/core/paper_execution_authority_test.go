package main

import (
	"database/sql"
	"testing"
)

func downgradePaperAuthorizationForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	downgradePaperPerformanceForTest(t, db)
	if _, err := db.Exec(`DROP TRIGGER paper_execution_authorizations_no_update;
		DROP TRIGGER paper_execution_authorizations_no_delete;
		DROP TRIGGER paper_execution_authorizations_state_guard;
		DROP TRIGGER paper_signal_events_capitalized_quantity_guard;
		DROP TRIGGER order_idempotency_capitalized_paper_guard;
		DROP TRIGGER order_idempotency_legacy_paper_signal_guard;
		DROP TRIGGER order_events_risk_reservation_guard;
		DROP TRIGGER order_events_dispatch_reservation_guard;
		DROP TRIGGER order_events_non_authority_reservation_guard;
		DROP TRIGGER order_events_capitalized_paper_fill_guard;
		DROP TABLE paper_execution_authorizations;
		ALTER TABLE order_events DROP COLUMN paper_authorization_id;
		CREATE TRIGGER order_idempotency_legacy_paper_signal_guard BEFORE INSERT ON order_idempotency
		WHEN NEW.mode='paper' AND json_extract(NEW.intent_json, '$.signal_schema_version') IN ('paper-signal.v1','paper-signal.v2')
		AND EXISTS (SELECT 1 FROM paper_signal_events WHERE account_ref=NEW.account_ref)
		BEGIN SELECT RAISE(ABORT, 'legacy paper order cannot follow a v3 signal'); END;
		CREATE TRIGGER order_events_risk_reservation_guard BEFORE INSERT ON order_events WHEN NEW.event_type='RISK_APPROVED' BEGIN
			SELECT CASE WHEN NEW.authority_reservation_id IS NULL OR NOT EXISTS (SELECT 1 FROM risk_reservations WHERE reservation_id=NEW.authority_reservation_id AND order_id=NEW.order_id AND risk_event_id=NEW.event_id AND reservation_id=json_extract(NEW.event_json, '$.risk_reservation_id') AND policy_version=json_extract(NEW.event_json, '$.risk_policy_version') AND fencing_token=json_extract(NEW.event_json, '$.fencing_token')) THEN RAISE(ABORT, 'risk approval requires an authority reservation') END;
		END;
		CREATE TRIGGER order_events_dispatch_reservation_guard BEFORE INSERT ON order_events WHEN NEW.event_type='SUBMIT_DISPATCHED' BEGIN
			SELECT CASE WHEN NEW.authority_reservation_id IS NULL OR NOT EXISTS (SELECT 1 FROM risk_reservations WHERE reservation_id=NEW.authority_reservation_id AND order_id=NEW.order_id AND dispatch_event_id=NEW.event_id AND reservation_id=json_extract(NEW.event_json, '$.risk_reservation_id') AND policy_version=json_extract(NEW.event_json, '$.risk_policy_version') AND fencing_token=json_extract(NEW.event_json, '$.fencing_token')) THEN RAISE(ABORT, 'submit dispatch requires an authority reservation') END;
		END;
		CREATE TRIGGER order_events_non_authority_reservation_guard BEFORE INSERT ON order_events WHEN NEW.event_type NOT IN ('RISK_APPROVED','SUBMIT_DISPATCHED') AND NEW.authority_reservation_id IS NOT NULL BEGIN
			SELECT RAISE(ABORT, 'authority reservation is invalid for this event');
		END;
		DELETE FROM schema_migrations WHERE version=17`); err != nil {
		t.Fatal(err)
	}
}
