package sqlite

import (
	"fmt"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/internal/storage/ownership"
	"strings"
)

var recoveryReadSQL = []string{
	"SELECT (SELECT COUNT(*) FROM paper_promotion_claims) +\n\t\t(SELECT COUNT(*) FROM paper_promotion_decisions) + (SELECT COUNT(*) FROM strategy_selection_events WHERE paper_promotion_attempt_id IS NOT NULL)",
	"SELECT 0,performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,\n\t\tstrategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,\n\t\tpaper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,\n\t\tunrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,\n\t\tmax_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events\n\t\tWHERE account_ref=? AND paper_accounting_session_id=? AND strategy_selection_event_id=? AND sequence<=?\n\t\tORDER BY sequence",
	"SELECT 0,performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,\n\t\tstrategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,\n\t\tpaper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,\n\t\tunrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,\n\t\tmax_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events\n\t\tWHERE policy_version=? AND account_ref=? AND as_of=?",
	"SELECT 0,strategy_performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,\n\t\texpected_previous_strategy_performance_id,baseline_performance_id,latest_performance_id,baseline_as_of,\n\t\tlatest_as_of,sample_count,baseline_equity,latest_equity,peak_equity,period_return_state,period_return,\n\t\tcumulative_return,drawdown,max_drawdown,record_sha256,record_json,recorded_at\n\t\tFROM paper_strategy_performance_events WHERE policy_version=? AND account_ref=?\n\t\tAND strategy_selection_event_id=? AND latest_performance_id=?",
	"SELECT COALESCE((SELECT attempt_id FROM paper_promotion_claims ORDER BY sequence DESC LIMIT 1),'no_attempt')",
	"SELECT COALESCE(MAX(as_of),'') FROM paper_performance_events WHERE account_ref=?",
	"SELECT COALESCE(MAX(as_of),?),COUNT(*) FROM paper_performance_events WHERE account_ref=?",
	"SELECT COALESCE(MAX(close_at),'') FROM paper_market_bar_observations\n\t\tWHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=?",
	"SELECT COALESCE(MAX(sequence), 0) FROM paper_evaluation_events",
	"SELECT COALESCE(MAX(sequence),0) FROM execution_authority_events",
	"SELECT COALESCE(MAX(sequence),0) FROM order_events",
	"SELECT COALESCE(MAX(sequence),0) FROM paper_market_bar_observations",
	"SELECT COALESCE(MAX(sequence),0) FROM paper_market_bar_observations\n\t\tWHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=? AND sequence<=?",
	"SELECT COALESCE(MAX(sequence),0) FROM paper_strategy_performance_events",
	"SELECT COALESCE(MAX(sequence),0) FROM strategy_research_evidence",
	"SELECT COALESCE(MAX(sequence),0) FROM strategy_selection_events",
	"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	"SELECT COALESCE(MAX(version),0) FROM schema_migrations",
	"SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations",
	"SELECT COUNT(*) FROM order_events WHERE paper_authorization_id IS NOT NULL",
	"SELECT COUNT(*) FROM order_events WHERE sequence=?",
	"SELECT COUNT(*) FROM order_events events\n\t\t\tJOIN order_idempotency orders ON orders.order_id=events.order_id\n\t\t\tWHERE events.event_type='FILL_RECORDED' AND orders.mode='paper'\n\t\t\tAND json_extract(orders.intent_json, '$.signal_schema_version')='paper-signal.v3'",
	"SELECT COUNT(*) FROM order_idempotency WHERE mode='paper'\n\t\t\tAND json_extract(intent_json, '$.signal_schema_version') IN ('paper-signal.v1','paper-signal.v2')",
	"SELECT COUNT(*) FROM order_idempotency WHERE mode='paper' AND account_ref=?",
	"SELECT COUNT(*) FROM order_idempotency WHERE mode='paper' AND account_ref=?\n\t\tAND json_extract(intent_json, '$.signal_schema_version') IN ('paper-signal.v1','paper-signal.v2')",
	"SELECT COUNT(*) FROM paper_accounting_sessions",
	"SELECT COUNT(*) FROM paper_market_bar_observations WHERE sequence=?",
	"SELECT COUNT(*) FROM paper_performance_events p\n\t\tWHERE NOT EXISTS (\n\t\t\tSELECT 1 FROM paper_strategy_performance_events w\n\t\t\tJOIN paper_performance_policy_events e ON e.account_ref=w.account_ref\n\t\t\tAND e.strategy_selection_event_id=w.strategy_selection_event_id\n\t\t\tAND e.strategy_performance_id=w.strategy_performance_id\n\t\t\tWHERE w.account_ref=p.account_ref AND w.strategy_selection_event_id=p.strategy_selection_event_id\n\t\t\tAND w.latest_performance_id=p.performance_id\n\t\t)",
	"SELECT COUNT(*) FROM paper_promotion_claims WHERE (?=0 OR sequence<?) AND json_extract(record_json,'$.comparison_end')>=?",
	"SELECT COUNT(*) FROM paper_promotion_claims c WHERE NOT EXISTS(SELECT 1 FROM paper_promotion_decisions d WHERE d.attempt_id=c.attempt_id)",
	"SELECT COUNT(*) FROM pragma_table_info('order_events') WHERE name='paper_authorization_id'",
	"SELECT COUNT(*) FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger')",
	"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='paper_evaluation_events'",
	"SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND tbl_name='paper_runner_leases'",
	"SELECT COUNT(*) FROM sqlite_temp_master WHERE name=? OR tbl_name=?",
	"SELECT COUNT(*) FROM strategy_research_evidence WHERE result_sha256 IN (?,?) AND sequence<=?",
	"SELECT COUNT(*) FROM strategy_research_evidence WHERE sequence=?",
	"SELECT COUNT(*), COALESCE(MAX(sequence), 0) FROM execution_authority_events",
	"SELECT COUNT(*), COALESCE(MAX(sequence), 0) FROM strategy_selection_events",
	"SELECT COUNT(*),COUNT(*) FILTER (WHERE owner_id IS NOT NULL) FROM paper_runner_leases",
	"SELECT COUNT(*),COUNT(DISTINCT authority_reservation_id) FROM order_events WHERE authority_reservation_id IS NOT NULL",
	"SELECT DISTINCT account_ref FROM execution_authority_events ORDER BY account_ref",
	"SELECT DISTINCT close_at FROM paper_market_bar_observations\n\t\tWHERE source='paper_fixture' AND venue='KRX' AND currency='KRW' AND interval='1d'\n\t\tAND timezone='Asia/Seoul' AND price_adjustment='unspecified' AND (close_at>? OR (close_at=? AND ?)) AND close_at<=?\n\t\tAND (close_at>=? OR (close_at=? AND ?))\n\t\tAND source_available_at<=? AND fetched_at<=? AND recorded_at<=? ORDER BY close_at",
	"SELECT EXISTS(\n\t\tSELECT 1 FROM paper_performance_events p\n\t\tWHERE p.account_ref=? AND p.strategy_selection_event_id=?\n\t\t  AND NOT EXISTS(\n\t\t\tSELECT 1 FROM paper_strategy_performance_events w\n\t\t\tJOIN paper_performance_policy_events e\n\t\t\t  ON e.account_ref=w.account_ref\n\t\t\t AND e.strategy_selection_event_id=w.strategy_selection_event_id\n\t\t\t AND e.strategy_performance_id=w.strategy_performance_id\n\t\t\tWHERE w.account_ref=p.account_ref\n\t\t\t  AND w.strategy_selection_event_id=p.strategy_selection_event_id\n\t\t\t  AND w.latest_performance_id=p.performance_id\n\t\t  )\n\t)",
	"SELECT EXISTS(SELECT 1 FROM order_idempotency WHERE account_ref=? AND mode<>'paper')",
	"SELECT EXISTS(SELECT 1 FROM paper_market_bar_observations\n\t\t\tWHERE sequence<=? AND source='paper_fixture' AND venue='KRX' AND currency='KRW' AND interval='1d'\n\t\t\t  AND timezone='Asia/Seoul' AND price_adjustment='unspecified' AND close_at=?\n\t\t\t  AND source_available_at<=? AND fetched_at<=? AND recorded_at<=?)",
	"SELECT MAX(json_extract(c.record_json,'$.comparison_end')) FROM paper_promotion_decisions d\n\t\tJOIN paper_promotion_claims c ON c.attempt_id=d.attempt_id JOIN strategy_selection_events s ON s.event_id=d.selection_event_id\n\t\tWHERE s.selected_result_sha256=? AND s.sequence<=(SELECT sequence FROM strategy_selection_events WHERE event_id=?)",
	"SELECT MAX(recorded_at) FROM paper_promotion_claims",
	"SELECT account_ref FROM execution_authority_events WHERE event_id=?",
	"SELECT account_ref FROM paper_accounting_sessions ORDER BY sequence",
	"SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?",
	"SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=? AND target='paper_candidate'",
	"SELECT artifact_json FROM strategy_research_evidence WHERE sequence<=? ORDER BY sequence",
	"SELECT artifact_json,recorded_at FROM strategy_research_evidence WHERE result_sha256=?",
	"SELECT attempt_id FROM paper_promotion_claims ORDER BY sequence DESC LIMIT 1",
	"SELECT authorization_id,schema_version,account_ref,paper_accounting_session_id,execution_policy_sha256,\n\t\tpolicy_version,side,quantity,authority_event_id,fencing_token,risk_event_id,dispatch_event_id,record_sha256,record_json,authorized_at\n\t\tFROM paper_execution_authorizations WHERE order_id=?",
	"SELECT b.close_at FROM order_events e JOIN order_idempotency o ON o.order_id=e.order_id JOIN paper_market_bar_observations b ON b.observation_id=json_extract(e.event_json,'$.paper_bar_observation_id') WHERE e.sequence<=? AND o.account_ref=?",
	"SELECT close_at FROM paper_market_bar_observations\n\t\tWHERE source='paper_fixture' AND venue='KRX' AND currency='KRW' AND interval='1d'\n\t\t  AND timezone='Asia/Seoul' AND price_adjustment='unspecified'\n\t\t  AND source_available_at<=? AND fetched_at<=? AND recorded_at<=?\n\t\tGROUP BY close_at ORDER BY close_at DESC LIMIT 1",
	"SELECT decision,rollback_selection_event_id FROM paper_performance_policy_events WHERE policy_event_id=?",
	"SELECT evaluation_id FROM paper_evaluation_events WHERE account_ref=? AND strategy_selection_event_id=? ORDER BY sequence DESC LIMIT 1",
	"SELECT event_id FROM execution_authority_events WHERE account_ref=? ORDER BY sequence DESC LIMIT 1",
	"SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,NULL,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events WHERE account_ref=? ORDER BY sequence",
	"SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,NULL,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events WHERE event_id=?",
	"SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events WHERE account_ref=? ORDER BY sequence",
	"SELECT event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events WHERE event_id=?",
	"SELECT event_id,paper_performance_policy_event_id FROM execution_authority_events WHERE reason_code='automatic_performance_halt' ORDER BY sequence",
	"SELECT event_id,paper_performance_policy_event_id FROM strategy_selection_events WHERE reason_code='automatic_performance_rollback' ORDER BY sequence",
	"SELECT event_id,paper_promotion_attempt_id FROM strategy_selection_events WHERE paper_promotion_attempt_id IS NOT NULL OR reason_code='auto_comparison'",
	"SELECT event_id,schema_version,account_ref,paper_accounting_session_id,strategy_result_sha256,\n\t\tstrategy_selection_event_id,execution_policy_sha256,signal_id,signal_bar_observation_id,data_sha256,symbol,target_quantity,\n\t\tdata_as_of,generated_at,expires_at,market_observation_sequence_cutoff,record_sha256,record_json,recorded_at\n\t\tFROM paper_signal_events WHERE account_ref=? AND signal_id=?",
	"SELECT event_id,selected_result_sha256 FROM strategy_selection_events WHERE sequence=?",
	"SELECT event_json FROM order_events WHERE order_id=? ORDER BY sequence",
	"SELECT event_json FROM order_events WHERE provider_execution_ref=?",
	"SELECT event_sha256 FROM order_events WHERE event_id=?",
	"SELECT event_type,source_event_id,previous_selected_result_sha256,reason_code,paper_performance_policy_event_id\n\t\tFROM strategy_selection_events WHERE event_id=?",
	"SELECT events.event_json FROM order_events events\n\t\tJOIN order_idempotency orders ON orders.order_id=events.order_id\n\t\tWHERE events.event_type='FILL_RECORDED' AND orders.mode='paper' AND orders.account_ref=?\n\t\t  AND json_extract(orders.intent_json, '$.signal_schema_version')='paper-signal.v3'\n\t\t  AND json_extract(orders.intent_json, '$.symbol')=?\n\t\t  AND json_extract(events.event_json, '$.paper_bar_observation_id')=?",
	"SELECT events.sequence,events.event_sha256,events.event_json,events.recorded_at,orders.order_id\n\t\tFROM order_events events JOIN order_idempotency orders ON orders.order_id=events.order_id\n\t\tWHERE events.event_type='FILL_RECORDED' AND orders.mode='paper'\n\t\t  AND json_extract(orders.intent_json, '$.signal_schema_version')='paper-signal.v3'\n\t\tORDER BY events.sequence",
	"SELECT events.sequence,events.event_type,events.event_json,orders.order_id\n\t\tFROM order_events events JOIN order_idempotency orders ON orders.order_id=events.order_id\n\t\tWHERE orders.mode='paper' AND json_extract(orders.intent_json, '$.signal_schema_version')='paper-signal.v3'\n\t\tORDER BY events.sequence",
	"SELECT intent_json FROM order_idempotency WHERE order_id=?",
	"SELECT json_extract(artifact_json,'$.manifest.strategy.parameters.slow_window'),json_extract(artifact_json,'$.input_sha256') FROM strategy_research_evidence WHERE result_sha256=?",
	"SELECT json_extract(event_json, '$.paper_bar_observation_id') FROM order_events\n\t\tWHERE order_id=? AND event_type='FILL_RECORDED' AND json_extract(event_json, '$.paper_fill_policy_version')=?",
	"SELECT observation_id FROM paper_market_bar_observations\n\t\tWHERE sequence<=? AND close_at=? ORDER BY sequence",
	"SELECT observation_id FROM paper_market_bar_observations\n\t\tWHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=? AND sequence>?\n\t\tORDER BY sequence LIMIT -1 OFFSET ?",
	"SELECT observation_id,close_at,close FROM paper_market_bar_observations\n\t\tWHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=?\n\t\tORDER BY close_at DESC,sequence DESC LIMIT ?",
	"SELECT observation_id,schema_version,source,source_observation_id,input_data_sha256,symbol,venue,currency,\n\t\tinterval,timezone,price_adjustment,open,high,low,close,volume,open_at,close_at,source_available_at,fetched_at,record_sha256,record_json,recorded_at\n\t\tFROM paper_market_bar_observations WHERE source=? AND source_observation_id=?",
	"SELECT observation_id,schema_version,source,source_observation_id,input_data_sha256,symbol,venue,currency,\n\t\tinterval,timezone,price_adjustment,open,high,low,close,volume,open_at,close_at,source_available_at,fetched_at,recorded_at\n\t\tFROM paper_market_bar_observations WHERE source=? AND symbol=? AND venue=? AND interval=? AND timezone=? AND price_adjustment=?\n\t\tAND close_at>=? AND close_at<=? ORDER BY close_at,sequence",
	"SELECT order_id FROM order_events WHERE provider_order_ref=? LIMIT 1",
	"SELECT order_id FROM order_idempotency WHERE account_ref=?",
	"SELECT order_id FROM order_idempotency WHERE account_ref=? AND mode='paper' ORDER BY recorded_at,order_id",
	"SELECT order_id FROM order_idempotency WHERE account_ref=? ORDER BY order_id",
	"SELECT order_id FROM order_idempotency WHERE mode='paper' AND account_ref=? ORDER BY order_id",
	"SELECT order_id FROM order_idempotency WHERE mode='paper' AND account_ref=? ORDER BY rowid",
	"SELECT order_id FROM order_idempotency WHERE provider=? AND mode=? AND account_ref=? AND client_order_id=?",
	"SELECT order_id FROM risk_reservations WHERE account_ref=? ORDER BY sequence",
	"SELECT orders.order_id,latest.recorded_at\n\t\tFROM order_idempotency orders\n\t\tJOIN order_events latest ON latest.sequence=(\n\t\t\tSELECT MAX(sequence) FROM order_events WHERE order_id=orders.order_id\n\t\t)\n\t\tORDER BY latest.sequence DESC",
	"SELECT performance_id,paper_accounting_session_id,as_of,equity\n\t\tFROM paper_performance_events WHERE account_ref=? ORDER BY sequence",
	"SELECT policy_event_id FROM paper_performance_policy_events\n\t\tWHERE account_ref=? AND strategy_selection_event_id=? ORDER BY sequence DESC LIMIT 1",
	"SELECT policy_event_id FROM paper_performance_policy_events ORDER BY sequence DESC LIMIT 1",
	"SELECT previous_selected_result_sha256 FROM strategy_selection_events WHERE event_id=?",
	"SELECT provider,mode,account_ref,client_order_id,request_sha256,order_id,intent_json,recorded_at\n\t\tFROM order_idempotency ORDER BY provider,mode,account_ref,client_order_id",
	"SELECT record_json FROM paper_promotion_claims WHERE attempt_id=?",
	"SELECT record_json FROM paper_promotion_decisions WHERE attempt_id=?",
	"SELECT record_json,record_sha256 FROM strategy_selection_events WHERE event_id=? AND reason_code='auto_comparison' AND paper_promotion_attempt_id=?",
	"SELECT recorded_at FROM order_events WHERE order_id=? AND event_type='SUBMIT_DISPATCHED' ORDER BY sequence LIMIT 1",
	"SELECT recorded_at FROM paper_promotion_decisions WHERE attempt_id=?",
	"SELECT recorded_at FROM strategy_selection_events WHERE event_id=?",
	"SELECT recorded_at FROM strategy_selection_events WHERE event_id=? AND selected_result_sha256=?",
	"SELECT request_sha256, order_id FROM order_idempotency WHERE provider=? AND mode=? AND account_ref=? AND client_order_id=?",
	"SELECT reservation_id,account_ref,policy_version,authority_event_id,fencing_token,quantity,limit_price,limit_notional,risk_event_id,dispatch_event_id,record_sha256,record_json,reserved_at\n\t\tFROM risk_reservations WHERE order_id=?",
	"SELECT schema_version,policy_version,account_ref,strategy_result_sha256,strategy_selection_event_id,\n\t\texpected_previous_evaluation_id,paper_order_state_sha256,order_count,terminal_order_count,active_order_count,\n\t\tpending_action_count,decision,reason_code,record_sha256,record_json,recorded_at\n\t\tFROM paper_evaluation_events WHERE evaluation_id=?",
	"SELECT scope,fencing_token,owner_id,account_ref,heartbeat_at_ns,expires_at_ns,\n\t\tstrategy_selection_event_id,selected_result_sha256,record_sha256,record_json\n\t\tFROM paper_runner_leases WHERE scope=?",
	"SELECT selected_result_sha256 FROM strategy_selection_events WHERE event_id=?",
	"SELECT selected_result_sha256,recorded_at FROM strategy_selection_events WHERE event_id=?",
	"SELECT sequence FROM order_events WHERE event_id=?",
	"SELECT sequence FROM paper_performance_events WHERE performance_id=?",
	"SELECT sequence,account_ref,event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,NULL,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events ORDER BY sequence",
	"SELECT sequence,account_ref,event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events ORDER BY sequence",
	"SELECT sequence,account_ref,event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events WHERE paper_performance_policy_event_id=? ORDER BY sequence",
	"SELECT sequence,account_ref,event_id,armed,lease_owner,fencing_token,lease_expires_at,reason_code,paper_performance_policy_event_id,record_sha256,record_json,recorded_at\n\t\tFROM execution_authority_events WHERE sequence<=? ORDER BY sequence",
	"SELECT sequence,attempt_id,record_sha256,record_json,recorded_at FROM paper_promotion_claims ORDER BY sequence",
	"SELECT sequence,attempt_id,selection_event_id,record_sha256,record_json,recorded_at FROM paper_promotion_decisions ORDER BY sequence",
	"SELECT sequence,evaluation_id,schema_version,policy_version,account_ref,\n\t\tstrategy_result_sha256,strategy_selection_event_id,expected_previous_evaluation_id,paper_order_state_sha256,\n\t\torder_count,terminal_order_count,active_order_count,pending_action_count,decision,reason_code,record_sha256,record_json,recorded_at\n\t\tFROM paper_evaluation_events ORDER BY sequence",
	"SELECT sequence,event_id,event_sha256,order_id,event_type,source,provider_order_ref,provider_execution_ref,event_json,recorded_at,\n\t\t\tauthority_reservation_id,paper_authorization_id FROM order_events ORDER BY sequence",
	"SELECT sequence,event_id,event_sha256,order_id,event_type,source,provider_order_ref,provider_execution_ref,event_json,recorded_at,\n\t\tauthority_reservation_id FROM order_events ORDER BY sequence",
	"SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,\n\t\t source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,0,NULL,NULL,record_sha256,record_json,recorded_at\n\t\tFROM strategy_selection_events ORDER BY sequence",
	"SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,\n\t\t source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,NULL,NULL,record_sha256,record_json,recorded_at\n\t\tFROM strategy_selection_events ORDER BY sequence",
	"SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,\n\t\t source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,NULL,record_sha256,record_json,recorded_at\n\t\tFROM strategy_selection_events ORDER BY sequence",
	"SELECT sequence,event_id,schema_version,account_ref,paper_accounting_session_id,\n\t\tstrategy_result_sha256,strategy_selection_event_id,execution_policy_sha256,signal_id,signal_bar_observation_id,data_sha256,\n\t\tsymbol,target_quantity,data_as_of,generated_at,expires_at,market_observation_sequence_cutoff,record_sha256,record_json,recorded_at\n\t\tFROM paper_signal_events ORDER BY sequence",
	"SELECT sequence,event_id,selected_result_sha256\n\t\tFROM strategy_selection_events ORDER BY sequence DESC LIMIT 1",
	"SELECT sequence,event_sha256,order_id,event_type,source,event_json,authority_reservation_id FROM order_events WHERE event_id=?",
	"SELECT sequence,event_sha256,order_id,event_type,source,event_json,authority_reservation_id,paper_authorization_id\n\t\tFROM order_events WHERE event_id=?",
	"SELECT sequence,event_type,selected_result_sha256 FROM strategy_selection_events WHERE event_id=?",
	"SELECT sequence,event_type,source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_performance_policy_event_id,recorded_at\n\t\tFROM strategy_selection_events WHERE event_id=?",
	"SELECT sequence,observation_id,schema_version,source,source_observation_id,input_data_sha256,\n\t\tsymbol,venue,currency,interval,timezone,price_adjustment,open,high,low,close,volume,open_at,close_at,source_available_at,\n\t\tfetched_at,record_sha256,record_json,recorded_at FROM paper_market_bar_observations ORDER BY sequence",
	"SELECT sequence,observation_id,schema_version,source,source_observation_id,input_data_sha256,symbol,venue,currency,\n\t\tinterval,timezone,price_adjustment,open,high,low,close,volume,open_at,close_at,source_available_at,fetched_at,record_sha256,record_json,recorded_at\n\t\tFROM paper_market_bar_observations WHERE observation_id=?",
	"SELECT sequence,order_id FROM paper_execution_authorizations ORDER BY sequence",
	"SELECT sequence,order_id,reservation_id,account_ref,policy_version,authority_event_id,fencing_token,quantity,limit_price,limit_notional,risk_event_id,dispatch_event_id,record_sha256,record_json,reserved_at\n\t\tFROM risk_reservations ORDER BY sequence",
	"SELECT sequence,performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,\n\t\tstrategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,\n\t\tpaper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,\n\t\tunrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,\n\t\tmax_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events\n\t\tWHERE account_ref=? ORDER BY sequence DESC LIMIT 1",
	"SELECT sequence,performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,\n\t\tstrategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,\n\t\tpaper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,\n\t\tunrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,\n\t\tmax_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events ORDER BY sequence",
	"SELECT sequence,policy_event_id,schema_version,policy_version,account_ref,paper_accounting_session_id,\n\t\tstrategy_selection_event_id,selected_strategy_result_ref,strategy_performance_id,baseline_performance_id,latest_performance_id,\n\t\tsample_count,expected_previous_policy_event_id,strategy_selection_sequence_cutoff,paper_strategy_performance_sequence_cutoff,\n\t\texecution_authority_sequence_cutoff,decision,reason_code,rollback_selection_event_id,automatic_halt_count,record_sha256,record_json,recorded_at\n\t\tFROM paper_performance_policy_events ORDER BY sequence",
	"SELECT sequence,policy_event_id,schema_version,policy_version,account_ref,paper_accounting_session_id,\n\t\tstrategy_selection_event_id,selected_strategy_result_ref,strategy_performance_id,baseline_performance_id,latest_performance_id,\n\t\tsample_count,expected_previous_policy_event_id,strategy_selection_sequence_cutoff,paper_strategy_performance_sequence_cutoff,\n\t\texecution_authority_sequence_cutoff,decision,reason_code,rollback_selection_event_id,automatic_halt_count,record_sha256,record_json,recorded_at\n\t\tFROM paper_performance_policy_events WHERE policy_event_id=?",
	"SELECT sequence,policy_event_id,schema_version,policy_version,account_ref,paper_accounting_session_id,\n\t\tstrategy_selection_event_id,selected_strategy_result_ref,strategy_performance_id,baseline_performance_id,latest_performance_id,\n\t\tsample_count,expected_previous_policy_event_id,strategy_selection_sequence_cutoff,paper_strategy_performance_sequence_cutoff,\n\t\texecution_authority_sequence_cutoff,decision,reason_code,rollback_selection_event_id,automatic_halt_count,record_sha256,record_json,recorded_at\n\t\tFROM paper_performance_policy_events WHERE policy_version=? AND account_ref=? AND strategy_selection_event_id=? AND strategy_performance_id=?",
	"SELECT sequence,recorded_at FROM order_events ORDER BY sequence",
	"SELECT sequence,recorded_at FROM strategy_research_evidence ORDER BY sequence",
	"SELECT sequence,result_sha256,artifact_sha256,strategy_name,strategy_version,\n\t\tparameter_sha256,target,artifact_json,recorded_at FROM strategy_research_evidence ORDER BY sequence",
	"SELECT sequence,session_id,schema_version,account_ref,strategy_result_sha256,strategy_selection_event_id,\n\t\texecution_policy_sha256,execution_policy_json,starting_cash,currency,record_sha256,record_json,recorded_at\n\t\tFROM paper_accounting_sessions ORDER BY sequence",
	"SELECT sequence,strategy_performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,\n\t\texpected_previous_strategy_performance_id,baseline_performance_id,latest_performance_id,baseline_as_of,\n\t\tlatest_as_of,sample_count,baseline_equity,latest_equity,peak_equity,period_return_state,period_return,\n\t\tcumulative_return,drawdown,max_drawdown,record_sha256,record_json,recorded_at\n\t\tFROM paper_strategy_performance_events ORDER BY sequence",
	"SELECT sequence,strategy_performance_id,schema_version,policy_version,account_ref,\n\t\tpaper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,\n\t\texpected_previous_strategy_performance_id,baseline_performance_id,latest_performance_id,baseline_as_of,\n\t\tlatest_as_of,sample_count,baseline_equity,latest_equity,peak_equity,period_return_state,period_return,\n\t\tcumulative_return,drawdown,max_drawdown,record_sha256,record_json,recorded_at\n\t\tFROM paper_strategy_performance_events WHERE strategy_performance_id=?",
	"SELECT session_id,schema_version,account_ref,strategy_result_sha256,strategy_selection_event_id,\n\t\texecution_policy_sha256,execution_policy_json,starting_cash,currency,record_sha256,record_json,recorded_at\n\t\tFROM paper_accounting_sessions WHERE account_ref=?",
	"SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name=?",
	"SELECT sql FROM sqlite_master WHERE type='index' AND name='strategy_selection_events_runner_binding_idx' AND tbl_name='strategy_selection_events'",
	"SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_runner_leases'",
	"SELECT sql FROM sqlite_master WHERE type='trigger' AND tbl_name='paper_runner_leases' AND name=?",
	"SELECT strategy_performance_id FROM paper_strategy_performance_events\n\t\tWHERE account_ref=? AND paper_accounting_session_id=? AND strategy_selection_event_id=?\n\t\tORDER BY sequence DESC LIMIT 1",
	"SELECT strategy_performance_id FROM paper_strategy_performance_events\n\t\tWHERE account_ref=? AND sequence<=? ORDER BY sequence DESC LIMIT 1",
	"SELECT strategy_performance_id FROM paper_strategy_performance_events WHERE account_ref=? ORDER BY sequence DESC LIMIT 1",
	"SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='paper_runner_leases'",
	"SELECT target FROM strategy_research_evidence WHERE result_sha256=?",
}

func recoveryReadStatements() ([]observation.Statement, error) {
	objects := ownership.Tables("trading")
	objects = append(objects, "paper_promotion_claims_no_update", "paper_promotion_claims_no_delete", "paper_promotion_decisions_no_update", "paper_promotion_decisions_no_delete", "paper_runner_leases_no_delete", "paper_runner_leases_state_guard", "strategy_selection_events_runner_binding_idx")
	statements := make([]observation.Statement, 0, len(recoveryReadSQL))
	for _, query := range recoveryReadSQL {
		arguments, err := ownership.CatalogArguments(query)
		if err != nil {
			return nil, fmt.Errorf("trading catalog statement: %w", err)
		}
		statement := observation.Statement{SQL: query, Arguments: strings.Count(query, "?"), Objects: objects, ObjectArguments: arguments}
		statements = append(statements, statement)
	}
	return statements, nil
}
func (s *Store) BindRecoveryReaders(registry *observation.Registry) error {
	statements, err := recoveryReadStatements()
	if err != nil {
		return err
	}
	backup, err := registry.TradingReader(statements)
	if err != nil {
		return err
	}
	migration, err := registry.MigrationTradingReader(statements)
	if err != nil {
		return err
	}
	s.backupReader, s.migrationReader = backup, migration
	return nil
}
