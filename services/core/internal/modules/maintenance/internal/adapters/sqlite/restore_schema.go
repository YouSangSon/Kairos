package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"omni-folio/services/core/migrations"
	"strings"
)

func requireSchema(db *sql.DB) error {
	return requireSchemaVersionContext(context.Background(), db, latestSchema)
}
func requireOrderRestoreSchema(db *sql.DB) error {
	if err := requireSchema(db); err != nil {
		return fmt.Errorf("restore schema: %w", err)
	}
	for _, table := range []string{"events", "order_idempotency", "order_events", "execution_authority_events", "risk_reservations", "paper_execution_authorizations", "broker_snapshots", "broker_snapshot_reconciliations", "strategy_research_evidence", "strategy_selection_events", "paper_evaluation_events", "paper_accounting_sessions", "paper_market_bar_observations", "paper_signal_events", "paper_performance_events", "paper_strategy_performance_events", "paper_performance_policy_events", "fx_observations", "security_price_observations"} {
		var strict int
		if err := db.QueryRow(`SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?`, table).Scan(&strict); err != nil {
			return fmt.Errorf("restore order table %s: %w", table, err)
		}
		if strict != 1 {
			return fmt.Errorf("restore order table %s is not strict", table)
		}
	}
	var eventsDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='events'`).Scan(&eventsDefinition); err != nil {
		return fmt.Errorf("restore events definition: %w", err)
	}
	normalizedEvents := strings.ToLower(strings.Join(strings.Fields(eventsDefinition), " "))
	for _, required := range []string{
		"counter_currency text", "counter_amount text", "'fx_exchange'",
		"or (type = 'fx_exchange' and corrects_source_event_id is null",
		"amount <> '0' and amount <> '-0' and amount glob '-*'", "counter_currency <> currency",
		"counter_amount is not null and counter_amount <> '0' and counter_amount not glob '-*')",
	} {
		if !strings.Contains(normalizedEvents, required) {
			return errors.New("restore events lack the required FX exchange guard")
		}
	}
	for _, unique := range []struct {
		table   string
		columns []string
		origin  string
	}{
		{"order_idempotency", []string{"provider", "mode", "account_ref", "client_order_id"}, "pk"},
		{"order_idempotency", []string{"order_id"}, "u"},
		{"events", []string{"account_id", "source_event_id"}, "u"},
		{"events", []string{"account_id", "corrects_source_event_id"}, "u"},
		{"order_events", []string{"event_id"}, "u"},
		{"order_events", []string{"provider_execution_ref"}, "u"},
		{"execution_authority_events", []string{"event_id"}, "u"},
		{"execution_authority_events", []string{"account_ref", "fencing_token"}, "u"},
		{"risk_reservations", []string{"reservation_id"}, "u"},
		{"risk_reservations", []string{"order_id"}, "u"},
		{"risk_reservations", []string{"risk_event_id"}, "u"},
		{"risk_reservations", []string{"dispatch_event_id"}, "u"},
		{"paper_execution_authorizations", []string{"authorization_id"}, "u"},
		{"paper_execution_authorizations", []string{"order_id"}, "u"},
		{"paper_execution_authorizations", []string{"risk_event_id"}, "u"},
		{"paper_execution_authorizations", []string{"dispatch_event_id"}, "u"},
		{"broker_snapshots", []string{"snapshot_id"}, "u"},
		{"broker_snapshots", []string{"provider", "environment", "exchange", "account_ref", "fetched_at"}, "u"},
		{"broker_snapshot_reconciliations", []string{"reconciliation_id"}, "u"},
		{"broker_snapshot_reconciliations", []string{"snapshot_id", "ledger_account_id", "ledger_revision"}, "u"},
		{"strategy_research_evidence", []string{"result_sha256"}, "u"},
		{"strategy_selection_events", []string{"event_id"}, "u"},
		{"paper_evaluation_events", []string{"evaluation_id"}, "u"},
		{"paper_accounting_sessions", []string{"session_id"}, "u"},
		{"paper_accounting_sessions", []string{"account_ref"}, "u"},
		{"paper_market_bar_observations", []string{"observation_id"}, "u"},
		{"paper_market_bar_observations", []string{"source", "source_observation_id"}, "u"},
		{"paper_market_bar_observations", []string{"source", "symbol", "venue", "interval", "timezone", "price_adjustment", "open_at"}, "u"},
		{"paper_signal_events", []string{"event_id"}, "u"},
		{"paper_signal_events", []string{"account_ref", "signal_id"}, "u"},
		{"paper_performance_events", []string{"performance_id"}, "u"},
		{"paper_performance_events", []string{"policy_version", "account_ref", "paper_accounting_session_id", "as_of"}, "u"},
		{"paper_strategy_performance_events", []string{"strategy_performance_id"}, "u"},
		{"paper_strategy_performance_events", []string{"policy_version", "account_ref", "strategy_selection_event_id", "latest_performance_id"}, "u"},
		{"paper_performance_policy_events", []string{"policy_event_id"}, "u"},
		{"paper_performance_policy_events", []string{"policy_version", "account_ref", "strategy_selection_event_id", "strategy_performance_id"}, "u"},
		{"fx_observations", []string{"observation_id"}, "u"},
		{"fx_observations", []string{"source", "source_observation_id"}, "u"},
		{"fx_observations", []string{"source", "base_currency", "quote_currency", "observed_at"}, "u"},
		{"security_price_observations", []string{"observation_id"}, "u"},
		{"security_price_observations", []string{"source", "source_observation_id"}, "u"},
		{"security_price_observations", []string{"source", "instrument_id", "symbol", "venue", "currency", "observed_at", "price_adjustment"}, "u"},
		{"instrument_listing_events", []string{"event_id"}, "u"},
	} {
		ok, err := hasUniqueIndex(db, unique.table, unique.columns, unique.origin)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("restore order table %s lacks required unique columns %s", unique.table, strings.Join(unique.columns, ","))
		}
	}
	var fxIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='fx_observations_latest_idx' AND tbl_name='fx_observations'`).Scan(&fxIndexDefinition); err != nil {
		return fmt.Errorf("restore FX observation latest index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(fxIndexDefinition), " ")) !=
		"create index fx_observations_latest_idx on fx_observations(source, base_currency, quote_currency, observed_at desc, sequence desc)" {
		return errors.New("restore FX observations lack the required latest index")
	}
	var securityPriceIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='security_price_observations_latest_idx' AND tbl_name='security_price_observations'`).Scan(&securityPriceIndexDefinition); err != nil {
		return fmt.Errorf("restore security price observation latest index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(securityPriceIndexDefinition), " ")) !=
		"create index security_price_observations_latest_idx on security_price_observations(source, instrument_id, symbol, venue, currency, price_adjustment, observed_at desc, sequence desc)" {
		return errors.New("restore security price observations lack the required latest index")
	}
	var securityPriceTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='security_price_observations'`).Scan(&securityPriceTableDefinition); err != nil {
		return fmt.Errorf("restore security price observation definition: %w", err)
	}
	securityPriceMigration, err := migrations.Files.ReadFile("migrations/012_kiwoom_security_price_observations.sql")
	if err != nil {
		return fmt.Errorf("restore security price observation definition source: %w", err)
	}
	const securityPriceTablePrefix = "CREATE TABLE security_price_observations"
	tableStart := strings.Index(string(securityPriceMigration), securityPriceTablePrefix)
	if tableStart < 0 {
		return errors.New("restore security price observation definition source is invalid")
	}
	expectedSecurityPriceTable := strings.SplitN(string(securityPriceMigration)[tableStart:], "\n\nINSERT INTO", 2)[0]
	expectedSecurityPriceTable = strings.TrimSuffix(strings.TrimSpace(expectedSecurityPriceTable), ";")
	if strings.ToLower(strings.Join(strings.Fields(securityPriceTableDefinition), " ")) !=
		strings.ToLower(strings.Join(strings.Fields(expectedSecurityPriceTable), " ")) {
		return errors.New("restore security price observations lack the required table definition")
	}
	listingMigration, err := migrations.Files.ReadFile("migrations/013_instrument_listing_events.sql")
	if err != nil {
		return fmt.Errorf("restore instrument listing definition source: %w", err)
	}
	listingSQL := string(listingMigration)
	const listingTablePrefix = "CREATE TABLE instrument_listing_events"
	listingTableStart := strings.Index(listingSQL, listingTablePrefix)
	if listingTableStart < 0 {
		return errors.New("restore instrument listing definition source is invalid")
	}
	expectedListingTable := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(listingSQL[listingTableStart:], "\n\nCREATE INDEX", 2)[0]), ";")
	var listingTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='instrument_listing_events'`).Scan(&listingTableDefinition); err != nil {
		return fmt.Errorf("restore instrument listing definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(listingTableDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedListingTable), " ")) {
		return errors.New("restore instrument listings lack the required table definition")
	}
	var listingIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='instrument_listing_events_current_idx' AND tbl_name='instrument_listing_events'`).Scan(&listingIndexDefinition); err != nil {
		return fmt.Errorf("restore instrument listing current index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(listingIndexDefinition), " ")) !=
		"create index instrument_listing_events_current_idx on instrument_listing_events(venue, symbol, currency, sequence desc)" {
		return errors.New("restore instrument listings lack the required current index")
	}
	stateTriggerStart := strings.Index(listingSQL, "CREATE TRIGGER instrument_listing_events_state_guard")
	if stateTriggerStart < 0 {
		return errors.New("restore instrument listing state guard source is invalid")
	}
	expectedStateTrigger := strings.TrimSuffix(strings.TrimSpace(listingSQL[stateTriggerStart:]), ";")
	var stateTriggerDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='instrument_listing_events_state_guard' AND tbl_name='instrument_listing_events'`).Scan(&stateTriggerDefinition); err != nil {
		return fmt.Errorf("restore instrument listing state guard: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(stateTriggerDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedStateTrigger), " ")) {
		return errors.New("restore instrument listings lack the required state guard")
	}
	paperEvaluationMigration, err := migrations.Files.ReadFile("migrations/014_paper_evaluation_events.sql")
	if err != nil {
		return fmt.Errorf("restore paper evaluation definition source: %w", err)
	}
	paperEvaluationSQL := string(paperEvaluationMigration)
	const paperEvaluationTablePrefix = "CREATE TABLE paper_evaluation_events"
	paperEvaluationTableStart := strings.Index(paperEvaluationSQL, paperEvaluationTablePrefix)
	if paperEvaluationTableStart < 0 {
		return errors.New("restore paper evaluation definition source is invalid")
	}
	expectedPaperEvaluationTable := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(paperEvaluationSQL[paperEvaluationTableStart:], "\n\nALTER TABLE", 2)[0]), ";")
	var paperEvaluationTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_evaluation_events'`).Scan(&paperEvaluationTableDefinition); err != nil {
		return fmt.Errorf("restore paper evaluation definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(paperEvaluationTableDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedPaperEvaluationTable), " ")) {
		return errors.New("restore paper evaluations lack the required table definition")
	}
	var paperEvaluationSequenceType string
	var paperEvaluationSequenceNotNull int
	var paperEvaluationSequenceDefault sql.NullString
	if err := db.QueryRow(`SELECT type, "notnull", dflt_value FROM pragma_table_info('strategy_selection_events') WHERE name='paper_evaluation_sequence'`).
		Scan(&paperEvaluationSequenceType, &paperEvaluationSequenceNotNull, &paperEvaluationSequenceDefault); err != nil {
		return fmt.Errorf("restore strategy selection paper evaluation sequence: %w", err)
	}
	var strategySelectionTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='strategy_selection_events'`).Scan(&strategySelectionTableDefinition); err != nil {
		return fmt.Errorf("restore strategy selection definition: %w", err)
	}
	normalizedStrategySelectionTable := strings.ToLower(strings.Join(strings.Fields(strategySelectionTableDefinition), " "))
	if !strings.EqualFold(paperEvaluationSequenceType, "INTEGER") || paperEvaluationSequenceNotNull != 1 ||
		!paperEvaluationSequenceDefault.Valid || paperEvaluationSequenceDefault.String != "0" ||
		!strings.Contains(normalizedStrategySelectionTable, "paper_evaluation_sequence integer not null default 0 check (paper_evaluation_sequence >= 0)") {
		return errors.New("restore strategy selections lack the required paper evaluation sequence boundary")
	}
	var paperEvaluationIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='paper_evaluation_events_current_idx' AND tbl_name='paper_evaluation_events'`).Scan(&paperEvaluationIndexDefinition); err != nil {
		return fmt.Errorf("restore paper evaluation current index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(paperEvaluationIndexDefinition), " ")) !=
		"create index paper_evaluation_events_current_idx on paper_evaluation_events(account_ref, strategy_selection_event_id, sequence desc)" {
		return errors.New("restore paper evaluations lack the required current index")
	}
	paperStateTriggerStart := strings.Index(paperEvaluationSQL, "CREATE TRIGGER paper_evaluation_events_state_guard")
	if paperStateTriggerStart < 0 {
		return errors.New("restore paper evaluation state guard source is invalid")
	}
	expectedPaperStateTrigger := strings.TrimSuffix(strings.TrimSpace(paperEvaluationSQL[paperStateTriggerStart:]), ";")
	var paperStateTriggerDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='paper_evaluation_events_state_guard' AND tbl_name='paper_evaluation_events'`).Scan(&paperStateTriggerDefinition); err != nil {
		return fmt.Errorf("restore paper evaluation state guard: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(paperStateTriggerDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedPaperStateTrigger), " ")) {
		return errors.New("restore paper evaluations lack the required state guard")
	}
	paperAccountingMigration, err := migrations.Files.ReadFile("migrations/015_paper_accounting_sessions.sql")
	if err != nil {
		return fmt.Errorf("restore paper accounting definition source: %w", err)
	}
	paperAccountingSQL := string(paperAccountingMigration)
	const paperAccountingTablePrefix = "CREATE TABLE paper_accounting_sessions"
	paperAccountingTableStart := strings.Index(paperAccountingSQL, paperAccountingTablePrefix)
	if paperAccountingTableStart < 0 {
		return errors.New("restore paper accounting definition source is invalid")
	}
	expectedPaperAccountingTable := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(paperAccountingSQL[paperAccountingTableStart:], "\n\nCREATE TRIGGER", 2)[0]), ";")
	var paperAccountingTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_accounting_sessions'`).Scan(&paperAccountingTableDefinition); err != nil {
		return fmt.Errorf("restore paper accounting definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(paperAccountingTableDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedPaperAccountingTable), " ")) {
		return errors.New("restore paper accounting sessions lack the required table definition")
	}
	paperAccountingStateTriggerStart := strings.Index(paperAccountingSQL, "CREATE TRIGGER paper_accounting_sessions_state_guard")
	if paperAccountingStateTriggerStart < 0 {
		return errors.New("restore paper accounting state guard source is invalid")
	}
	expectedPaperAccountingStateTrigger := strings.TrimSuffix(strings.TrimSpace(paperAccountingSQL[paperAccountingStateTriggerStart:]), ";")
	var paperAccountingStateTriggerDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='paper_accounting_sessions_state_guard' AND tbl_name='paper_accounting_sessions'`).Scan(&paperAccountingStateTriggerDefinition); err != nil {
		return fmt.Errorf("restore paper accounting state guard: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(paperAccountingStateTriggerDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedPaperAccountingStateTrigger), " ")) {
		return errors.New("restore paper accounting sessions lack the required state guard")
	}
	paperMarketMigration, err := migrations.Files.ReadFile("migrations/016_paper_market_signals.sql")
	if err != nil {
		return fmt.Errorf("restore paper market definition source: %w", err)
	}
	paperMarketSQL := string(paperMarketMigration)
	barTableStart := strings.Index(paperMarketSQL, "CREATE TABLE paper_market_bar_observations")
	barTriggerStart := strings.Index(paperMarketSQL, "CREATE TRIGGER paper_market_bar_observations_no_update")
	signalTableStart := strings.Index(paperMarketSQL, "CREATE TABLE paper_signal_events")
	signalTriggerStart := strings.Index(paperMarketSQL, "CREATE TRIGGER paper_signal_events_no_update")
	legacyOrderGuardStart := strings.Index(paperMarketSQL, "CREATE TRIGGER order_idempotency_legacy_paper_signal_guard")
	signalStateTriggerStart := strings.Index(paperMarketSQL, "CREATE TRIGGER paper_signal_events_state_guard")
	if barTableStart < 0 || barTriggerStart <= barTableStart || signalTableStart <= barTriggerStart ||
		signalTriggerStart <= signalTableStart || legacyOrderGuardStart <= signalTriggerStart || signalStateTriggerStart <= legacyOrderGuardStart {
		return errors.New("restore paper market definition source is invalid")
	}
	expectedBarTable := strings.TrimSuffix(strings.TrimSpace(paperMarketSQL[barTableStart:barTriggerStart]), ";")
	expectedSignalTable := strings.TrimSuffix(strings.TrimSpace(paperMarketSQL[signalTableStart:signalTriggerStart]), ";")
	expectedPaperSignalStateTrigger := strings.TrimSuffix(strings.TrimSpace(paperMarketSQL[signalStateTriggerStart:]), ";")
	for table, expected := range map[string]string{
		"paper_market_bar_observations": expectedBarTable,
		"paper_signal_events":           expectedSignalTable,
	} {
		var definition string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&definition); err != nil {
			return fmt.Errorf("restore %s definition: %w", table, err)
		}
		if strings.ToLower(strings.Join(strings.Fields(definition), " ")) != strings.ToLower(strings.Join(strings.Fields(expected), " ")) {
			return fmt.Errorf("restore %s lacks the required table definition", table)
		}
	}
	paperAuthorizationMigration, err := migrations.Files.ReadFile("migrations/017_paper_execution_authorizations.sql")
	if err != nil {
		return fmt.Errorf("restore paper authorization definition source: %w", err)
	}
	paperAuthorizationSQL := string(paperAuthorizationMigration)
	authorizationTableStart := strings.Index(paperAuthorizationSQL, "CREATE TABLE paper_execution_authorizations")
	authorizationIndexStart := strings.Index(paperAuthorizationSQL, "CREATE INDEX paper_execution_authorizations_account_idx")
	paperSignalQuantityGuardStart := strings.Index(paperAuthorizationSQL, "CREATE TRIGGER paper_signal_events_capitalized_quantity_guard")
	legacyAuthorizationGuardStart := strings.Index(paperAuthorizationSQL, "CREATE TRIGGER order_idempotency_legacy_paper_signal_guard")
	capitalizedOrderGuardStart := strings.Index(paperAuthorizationSQL, "CREATE TRIGGER order_idempotency_capitalized_paper_guard")
	dropRiskGuardStart := strings.Index(paperAuthorizationSQL, "DROP TRIGGER order_events_risk_reservation_guard")
	riskGuardStart := strings.LastIndex(paperAuthorizationSQL, "CREATE TRIGGER order_events_risk_reservation_guard")
	dispatchGuardStart := strings.LastIndex(paperAuthorizationSQL, "CREATE TRIGGER order_events_dispatch_reservation_guard")
	nonAuthorityGuardStart := strings.LastIndex(paperAuthorizationSQL, "CREATE TRIGGER order_events_non_authority_reservation_guard")
	capitalizedFillGuardStart := strings.LastIndex(paperAuthorizationSQL, "CREATE TRIGGER order_events_capitalized_paper_fill_guard")
	authorizationStateGuardStart := strings.Index(paperAuthorizationSQL, "CREATE TRIGGER paper_execution_authorizations_state_guard")
	if authorizationTableStart < 0 || authorizationIndexStart <= authorizationTableStart || authorizationStateGuardStart < 0 ||
		paperSignalQuantityGuardStart < 0 || legacyAuthorizationGuardStart <= paperSignalQuantityGuardStart ||
		capitalizedOrderGuardStart <= legacyAuthorizationGuardStart || dropRiskGuardStart <= capitalizedOrderGuardStart ||
		riskGuardStart <= dropRiskGuardStart || dispatchGuardStart <= riskGuardStart || nonAuthorityGuardStart <= dispatchGuardStart ||
		capitalizedFillGuardStart <= nonAuthorityGuardStart {
		return errors.New("restore paper authorization definition source is invalid")
	}
	expectedAuthorizationTable := strings.TrimSuffix(strings.TrimSpace(paperAuthorizationSQL[authorizationTableStart:authorizationIndexStart]), ";")
	var authorizationTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_execution_authorizations'`).Scan(&authorizationTableDefinition); err != nil {
		return fmt.Errorf("restore paper authorization definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(authorizationTableDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedAuthorizationTable), " ")) {
		return errors.New("restore paper execution authorizations lack the required table definition")
	}
	extractAuthorizationTrigger := func(start, end int) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(paperAuthorizationSQL[start:end]), ";")), " "))
	}
	expectedAuthorizationStateGuard := extractAuthorizationTrigger(authorizationStateGuardStart, strings.Index(paperAuthorizationSQL[authorizationStateGuardStart:], "\n\nDROP TRIGGER")+authorizationStateGuardStart)
	expectedPaperSignalQuantityGuard := extractAuthorizationTrigger(paperSignalQuantityGuardStart, legacyAuthorizationGuardStart)
	expectedLegacyAuthorizationGuard := extractAuthorizationTrigger(legacyAuthorizationGuardStart, capitalizedOrderGuardStart)
	expectedCapitalizedOrderGuard := extractAuthorizationTrigger(capitalizedOrderGuardStart, dropRiskGuardStart)
	expectedRiskGuard := extractAuthorizationTrigger(riskGuardStart, dispatchGuardStart)
	expectedDispatchGuard := extractAuthorizationTrigger(dispatchGuardStart, nonAuthorityGuardStart)
	expectedNonAuthorityGuard := extractAuthorizationTrigger(nonAuthorityGuardStart, capitalizedFillGuardStart)
	expectedCapitalizedFillGuard := strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(paperAuthorizationSQL[capitalizedFillGuardStart:]), ";")), " "))
	performanceMigration, err := migrations.Files.ReadFile("migrations/018_paper_performance_events.sql")
	if err != nil {
		return fmt.Errorf("restore paper performance definition source: %w", err)
	}
	performanceSQL := string(performanceMigration)
	performanceTableStart := strings.Index(performanceSQL, "CREATE TABLE paper_performance_events")
	performanceIndexStart := strings.Index(performanceSQL, "CREATE INDEX paper_performance_events_account_idx")
	performanceUpdateTriggerStart := strings.Index(performanceSQL, "CREATE TRIGGER paper_performance_events_no_update")
	performanceStateTriggerStart := strings.Index(performanceSQL, "CREATE TRIGGER paper_performance_events_state_guard")
	if performanceTableStart < 0 || performanceIndexStart <= performanceTableStart ||
		performanceUpdateTriggerStart <= performanceIndexStart || performanceStateTriggerStart <= performanceUpdateTriggerStart {
		return errors.New("restore paper performance definition source is invalid")
	}
	expectedPerformanceTable := strings.TrimSuffix(strings.TrimSpace(performanceSQL[performanceTableStart:performanceIndexStart]), ";")
	var performanceTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_performance_events'`).Scan(&performanceTableDefinition); err != nil {
		return fmt.Errorf("restore paper performance definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(performanceTableDefinition), " ")) != strings.ToLower(strings.Join(strings.Fields(expectedPerformanceTable), " ")) {
		return errors.New("restore paper performance lacks the required table definition")
	}
	var performanceIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='paper_performance_events_account_idx' AND tbl_name='paper_performance_events'`).Scan(&performanceIndexDefinition); err != nil {
		return fmt.Errorf("restore paper performance account index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(performanceIndexDefinition), " ")) !=
		"create index paper_performance_events_account_idx on paper_performance_events(account_ref, sequence desc)" {
		return errors.New("restore paper performance lacks the required account index")
	}
	expectedPerformanceStateTrigger := strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(
		strings.TrimSpace(performanceSQL[performanceStateTriggerStart:]), ";")), " "))
	strategyPerformanceMigration, err := migrations.Files.ReadFile("migrations/019_paper_strategy_performance_events.sql")
	if err != nil {
		return fmt.Errorf("restore paper strategy performance definition source: %w", err)
	}
	strategyPerformanceSQL := string(strategyPerformanceMigration)
	strategyPerformanceTableStart := strings.Index(strategyPerformanceSQL, "CREATE TABLE paper_strategy_performance_events")
	strategyPerformanceIndexStart := strings.Index(strategyPerformanceSQL, "CREATE INDEX paper_strategy_performance_events_window_idx")
	strategyPerformanceUpdateTriggerStart := strings.Index(strategyPerformanceSQL, "CREATE TRIGGER paper_strategy_performance_events_no_update")
	strategyPerformanceStateTriggerStart := strings.Index(strategyPerformanceSQL, "CREATE TRIGGER paper_strategy_performance_events_state_guard")
	if strategyPerformanceTableStart < 0 || strategyPerformanceIndexStart <= strategyPerformanceTableStart ||
		strategyPerformanceUpdateTriggerStart <= strategyPerformanceIndexStart ||
		strategyPerformanceStateTriggerStart <= strategyPerformanceUpdateTriggerStart {
		return errors.New("restore paper strategy performance definition source is invalid")
	}
	expectedStrategyPerformanceTable := strings.TrimSuffix(strings.TrimSpace(
		strategyPerformanceSQL[strategyPerformanceTableStart:strategyPerformanceIndexStart]), ";")
	var strategyPerformanceTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_strategy_performance_events'`).Scan(&strategyPerformanceTableDefinition); err != nil {
		return fmt.Errorf("restore paper strategy performance definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(strategyPerformanceTableDefinition), " ")) !=
		strings.ToLower(strings.Join(strings.Fields(expectedStrategyPerformanceTable), " ")) {
		return errors.New("restore paper strategy performance lacks the required table definition")
	}
	var strategyPerformanceIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='paper_strategy_performance_events_window_idx' AND tbl_name='paper_strategy_performance_events'`).Scan(&strategyPerformanceIndexDefinition); err != nil {
		return fmt.Errorf("restore paper strategy performance window index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(strategyPerformanceIndexDefinition), " ")) !=
		"create index paper_strategy_performance_events_window_idx on paper_strategy_performance_events(account_ref, strategy_selection_event_id, sequence desc)" {
		return errors.New("restore paper strategy performance lacks the required window index")
	}
	policyMigrationForTable, err := migrations.Files.ReadFile("migrations/020_paper_performance_policy_events.sql")
	if err != nil {
		return fmt.Errorf("restore paper performance policy definition source: %w", err)
	}
	policySQLForTable := string(policyMigrationForTable)
	policyTableStart := strings.Index(policySQLForTable, "CREATE TABLE paper_performance_policy_events")
	policyIndexStart := strings.Index(policySQLForTable, "CREATE INDEX execution_authority_latest_idx")
	if policyTableStart < 0 || policyIndexStart <= policyTableStart {
		return errors.New("restore paper performance policy definition source is invalid")
	}
	expectedPolicyTable := strings.TrimSuffix(strings.TrimSpace(policySQLForTable[policyTableStart:policyIndexStart]), ";")
	var policyTableDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='paper_performance_policy_events'`).Scan(&policyTableDefinition); err != nil {
		return fmt.Errorf("restore paper performance policy definition: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(policyTableDefinition), " ")) !=
		strings.ToLower(strings.Join(strings.Fields(expectedPolicyTable), " ")) {
		return errors.New("restore paper performance policy lacks the required table definition")
	}
	var policyIndexDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='paper_performance_policy_events_account_selection_idx' AND tbl_name='paper_performance_policy_events'`).Scan(&policyIndexDefinition); err != nil {
		return fmt.Errorf("restore paper performance policy index: %w", err)
	}
	if strings.ToLower(strings.Join(strings.Fields(policyIndexDefinition), " ")) !=
		"create index paper_performance_policy_events_account_selection_idx on paper_performance_policy_events(account_ref, strategy_selection_event_id, sequence desc)" {
		return errors.New("restore paper performance policy lacks the required account selection index")
	}
	expectedStrategyPerformanceStateTrigger := strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(
		strings.TrimSpace(strategyPerformanceSQL[strategyPerformanceStateTriggerStart:]), ";")), " "))

	for _, table := range []string{"events", "order_events", "execution_authority_events", "risk_reservations", "paper_execution_authorizations", "broker_snapshots", "broker_snapshot_reconciliations", "strategy_research_evidence", "strategy_selection_events", "paper_evaluation_events", "paper_accounting_sessions", "paper_market_bar_observations", "paper_signal_events", "paper_performance_events", "paper_strategy_performance_events", "paper_performance_policy_events", "fx_observations", "security_price_observations", "instrument_listing_events"} {
		var sequenceType string
		var sequencePK, primaryKeyColumns, primaryKeyIndexes int
		if err := db.QueryRow(`SELECT type, pk FROM pragma_table_info(?) WHERE name='sequence'`, table).Scan(&sequenceType, &sequencePK); err != nil {
			return fmt.Errorf("restore %s sequence: %w", table, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE pk>0`, table).Scan(&primaryKeyColumns); err != nil {
			return fmt.Errorf("restore %s primary key: %w", table, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_index_list(?) WHERE origin='pk'`, table).Scan(&primaryKeyIndexes); err != nil {
			return fmt.Errorf("restore %s primary-key index: %w", table, err)
		}
		if !strings.EqualFold(sequenceType, "INTEGER") || sequencePK != 1 || primaryKeyColumns != 1 || primaryKeyIndexes != 0 {
			return fmt.Errorf("restore %s sequence is not the integer primary key", table)
		}
	}
	var foreignKeys, matchingForeignKeys int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(("table"='order_idempotency' AND "from"='order_id' AND "to"='order_id') OR ("table"='risk_reservations' AND "from"='authority_reservation_id' AND "to"='reservation_id') OR ("table"='paper_execution_authorizations' AND "from"='paper_authorization_id' AND "to"='authorization_id')), 0) FROM pragma_foreign_key_list(?)`, "order_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore order foreign key: %w", err)
	}
	if foreignKeys != 3 || matchingForeignKeys != 3 {
		return errors.New("restore order events lack required order or authority foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(("table"='order_idempotency' AND "from"='order_id' AND "to"='order_id') OR ("table"='execution_authority_events' AND "from"='authority_event_id' AND "to"='event_id')), 0) FROM pragma_foreign_key_list(?)`, "risk_reservations").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore risk reservation foreign keys: %w", err)
	}
	if foreignKeys != 2 || matchingForeignKeys != 2 {
		return errors.New("restore risk reservations lack required order or authority foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(
		("table"='order_idempotency' AND "from"='order_id' AND "to"='order_id') OR
		("table"='paper_accounting_sessions' AND "from"='paper_accounting_session_id' AND "to"='session_id') OR
		("table"='execution_authority_events' AND "from"='authority_event_id' AND "to"='event_id')), 0)
		FROM pragma_foreign_key_list(?)`, "paper_execution_authorizations").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper authorization foreign keys: %w", err)
	}
	if foreignKeys != 3 || matchingForeignKeys != 3 {
		return errors.New("restore paper authorizations lack required order, session, or authority foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM("table"='broker_snapshots' AND "from"='snapshot_id' AND "to"='snapshot_id'), 0) FROM pragma_foreign_key_list(?)`, "broker_snapshot_reconciliations").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore broker reconciliation foreign key: %w", err)
	}
	if foreignKeys != 1 || matchingForeignKeys != 1 {
		return errors.New("restore broker reconciliations lack the required snapshot foreign key")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(("table"='strategy_research_evidence' AND "from"='candidate_result_sha256' AND "to"='result_sha256') OR ("table"='strategy_selection_events' AND "from"='source_event_id' AND "to"='event_id') OR ("table"='paper_performance_policy_events' AND "from"='paper_performance_policy_event_id' AND "to"='policy_event_id') OR ("table"='paper_promotion_decisions' AND "from"='paper_promotion_attempt_id' AND "to"='attempt_id')), 0) FROM pragma_foreign_key_list(?)`, "strategy_selection_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore strategy selection foreign keys: %w", err)
	}
	if foreignKeys != 4 || matchingForeignKeys != 4 {
		return errors.New("restore strategy selection events lack required evidence, source, policy, or promotion foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM("table"='paper_performance_policy_events' AND "from"='paper_performance_policy_event_id' AND "to"='policy_event_id'), 0) FROM pragma_foreign_key_list(?)`, "execution_authority_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore execution authority policy foreign key: %w", err)
	}
	if foreignKeys != 1 || matchingForeignKeys != 1 {
		return errors.New("restore execution authority lacks the required policy foreign key")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(("table"='strategy_research_evidence' AND "from"='strategy_result_sha256' AND "to"='result_sha256') OR ("table"='strategy_selection_events' AND "from"='strategy_selection_event_id' AND "to"='event_id')), 0) FROM pragma_foreign_key_list(?)`, "paper_evaluation_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper evaluation foreign keys: %w", err)
	}
	if foreignKeys != 2 || matchingForeignKeys != 2 {
		return errors.New("restore paper evaluations lack required strategy foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(("table"='strategy_research_evidence' AND "from"='strategy_result_sha256' AND "to"='result_sha256') OR ("table"='strategy_selection_events' AND "from"='strategy_selection_event_id' AND "to"='event_id')), 0) FROM pragma_foreign_key_list(?)`, "paper_accounting_sessions").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper accounting foreign keys: %w", err)
	}
	if foreignKeys != 2 || matchingForeignKeys != 2 {
		return errors.New("restore paper accounting sessions lack required strategy foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(
		("table"='paper_accounting_sessions' AND "from"='paper_accounting_session_id' AND "to"='session_id') OR
		("table"='strategy_research_evidence' AND "from"='strategy_result_sha256' AND "to"='result_sha256') OR
		("table"='strategy_selection_events' AND "from"='strategy_selection_event_id' AND "to"='event_id') OR
		("table"='paper_market_bar_observations' AND "from"='signal_bar_observation_id' AND "to"='observation_id')), 0)
		FROM pragma_foreign_key_list(?)`, "paper_signal_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper signal foreign keys: %w", err)
	}
	if foreignKeys != 4 || matchingForeignKeys != 4 {
		return errors.New("restore paper signals lack required session, strategy, or bar foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(
		("table"='paper_accounting_sessions' AND "from"='paper_accounting_session_id' AND "to"='session_id') OR
		("table"='strategy_selection_events' AND "from"='strategy_selection_event_id' AND "to"='event_id')), 0)
		FROM pragma_foreign_key_list(?)`, "paper_performance_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper performance foreign keys: %w", err)
	}
	if foreignKeys != 2 || matchingForeignKeys != 2 {
		return errors.New("restore paper performance lacks required session or selection foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(
		("table"='paper_accounting_sessions' AND "from"='paper_accounting_session_id' AND "to"='session_id') OR
		("table"='strategy_selection_events' AND "from"='strategy_selection_event_id' AND "to"='event_id') OR
		("table"='strategy_research_evidence' AND "from"='selected_strategy_result_ref' AND "to"='result_sha256') OR
		("table"='paper_performance_events' AND "from"='baseline_performance_id' AND "to"='performance_id') OR
		("table"='paper_performance_events' AND "from"='latest_performance_id' AND "to"='performance_id')), 0)
		FROM pragma_foreign_key_list(?)`, "paper_strategy_performance_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper strategy performance foreign keys: %w", err)
	}
	if foreignKeys != 5 || matchingForeignKeys != 5 {
		return errors.New("restore paper strategy performance lacks required source foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(
		("table"='paper_accounting_sessions' AND "from"='paper_accounting_session_id' AND "to"='session_id') OR
		("table"='strategy_selection_events' AND "from"='strategy_selection_event_id' AND "to"='event_id') OR
		("table"='strategy_research_evidence' AND "from"='selected_strategy_result_ref' AND "to"='result_sha256') OR
		("table"='paper_strategy_performance_events' AND "from"='strategy_performance_id' AND "to"='strategy_performance_id') OR
		("table"='paper_performance_events' AND "from"='baseline_performance_id' AND "to"='performance_id') OR
		("table"='paper_performance_events' AND "from"='latest_performance_id' AND "to"='performance_id') OR
		("table"='strategy_selection_events' AND "from"='rollback_selection_event_id' AND "to"='event_id')), 0)
		FROM pragma_foreign_key_list(?)`, "paper_performance_policy_events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore paper performance policy foreign keys: %w", err)
	}
	if foreignKeys != 7 || matchingForeignKeys != 7 {
		return errors.New("restore paper performance policy lacks required source or rollback foreign keys")
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(("table"='events' AND "from"='account_id' AND "to"='account_id') OR ("table"='events' AND "from"='corrects_source_event_id' AND "to"='source_event_id')), 0) FROM pragma_foreign_key_list(?)`, "events").Scan(&foreignKeys, &matchingForeignKeys); err != nil {
		return fmt.Errorf("restore cash void foreign key: %w", err)
	}
	if foreignKeys != 2 || matchingForeignKeys != 2 {
		return errors.New("restore events lack the required cash-void foreign key")
	}
	foreignKeyRows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	if foreignKeyRows.Next() {
		foreignKeyRows.Close()
		return errors.New("restore candidate has broken foreign keys")
	}
	if err := foreignKeyRows.Err(); err != nil {
		foreignKeyRows.Close()
		return err
	}
	if err := foreignKeyRows.Close(); err != nil {
		return err
	}
	required := map[string]struct {
		table     string
		operation string
	}{
		"order_idempotency_no_update":                 {"order_idempotency", "update"},
		"order_idempotency_no_delete":                 {"order_idempotency", "delete"},
		"order_events_no_update":                      {"order_events", "update"},
		"order_events_no_delete":                      {"order_events", "delete"},
		"execution_authority_events_no_update":        {"execution_authority_events", "update"},
		"execution_authority_events_no_delete":        {"execution_authority_events", "delete"},
		"risk_reservations_no_update":                 {"risk_reservations", "update"},
		"risk_reservations_no_delete":                 {"risk_reservations", "delete"},
		"paper_execution_authorizations_no_update":    {"paper_execution_authorizations", "update"},
		"paper_execution_authorizations_no_delete":    {"paper_execution_authorizations", "delete"},
		"broker_snapshots_no_update":                  {"broker_snapshots", "update"},
		"broker_snapshots_no_delete":                  {"broker_snapshots", "delete"},
		"broker_snapshot_reconciliations_no_update":   {"broker_snapshot_reconciliations", "update"},
		"broker_snapshot_reconciliations_no_delete":   {"broker_snapshot_reconciliations", "delete"},
		"events_no_update":                            {"events", "update"},
		"events_no_delete":                            {"events", "delete"},
		"strategy_research_evidence_no_update":        {"strategy_research_evidence", "update"},
		"strategy_research_evidence_no_delete":        {"strategy_research_evidence", "delete"},
		"strategy_selection_events_no_update":         {"strategy_selection_events", "update"},
		"strategy_selection_events_no_delete":         {"strategy_selection_events", "delete"},
		"paper_evaluation_events_no_update":           {"paper_evaluation_events", "update"},
		"paper_evaluation_events_no_delete":           {"paper_evaluation_events", "delete"},
		"paper_accounting_sessions_no_update":         {"paper_accounting_sessions", "update"},
		"paper_accounting_sessions_no_delete":         {"paper_accounting_sessions", "delete"},
		"paper_market_bar_observations_no_update":     {"paper_market_bar_observations", "update"},
		"paper_market_bar_observations_no_delete":     {"paper_market_bar_observations", "delete"},
		"paper_signal_events_no_update":               {"paper_signal_events", "update"},
		"paper_signal_events_no_delete":               {"paper_signal_events", "delete"},
		"paper_performance_events_no_update":          {"paper_performance_events", "update"},
		"paper_performance_events_no_delete":          {"paper_performance_events", "delete"},
		"paper_strategy_performance_events_no_update": {"paper_strategy_performance_events", "update"},
		"paper_strategy_performance_events_no_delete": {"paper_strategy_performance_events", "delete"},
		"paper_performance_policy_events_no_update":   {"paper_performance_policy_events", "update"},
		"paper_performance_policy_events_no_delete":   {"paper_performance_policy_events", "delete"},
		"fx_observations_no_update":                   {"fx_observations", "update"},
		"fx_observations_no_delete":                   {"fx_observations", "delete"},
		"security_price_observations_no_update":       {"security_price_observations", "update"},
		"security_price_observations_no_delete":       {"security_price_observations", "delete"},
		"instrument_listing_events_no_update":         {"instrument_listing_events", "update"},
		"instrument_listing_events_no_delete":         {"instrument_listing_events", "delete"},
	}
	rows, err := db.Query(`SELECT name, tbl_name, sql FROM sqlite_master WHERE type='trigger'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, table, definition string
		if err := rows.Scan(&name, &table, &definition); err != nil {
			return err
		}
		expected, ok := required[name]
		normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
		expectedSQL := fmt.Sprintf("create trigger %s before %s on %s begin select raise(abort, '%s is insert-only'); end",
			name, expected.operation, expected.table, expected.table)
		if ok && expected.table == table && normalized == expectedSQL {
			delete(required, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(required) != 0 {
		return errors.New("restore candidate is missing insert-only state triggers")
	}
	policyMigration, err := migrations.Files.ReadFile("migrations/020_paper_performance_policy_events.sql")
	if err != nil {
		return fmt.Errorf("restore paper performance policy definition source: %w", err)
	}
	policyTriggerSQL := map[string]string{}
	for _, name := range []string{"strategy_selection_events_state_guard", "execution_authority_events_state_guard", "paper_performance_policy_events_state_guard"} {
		definition, err := migrationTriggerDefinition(string(policyMigration), name)
		if err != nil {
			return fmt.Errorf("restore paper performance policy trigger %s: %w", name, err)
		}
		policyTriggerSQL[name] = definition
	}
	promotionMigration, err := migrations.Files.ReadFile("migrations/027_paper_promotion.sql")
	if err != nil {
		return err
	}
	policyTriggerSQL["strategy_selection_events_state_guard"], err = migrationTriggerDefinition(string(promotionMigration), "strategy_selection_events_state_guard")
	if err != nil {
		return err
	}
	guardSQL := map[string]struct {
		table string
		sql   string
	}{
		"order_events_risk_reservation_guard":            {"order_events", expectedRiskGuard},
		"order_events_dispatch_reservation_guard":        {"order_events", expectedDispatchGuard},
		"order_events_non_authority_reservation_guard":   {"order_events", expectedNonAuthorityGuard},
		"order_events_capitalized_paper_fill_guard":      {"order_events", expectedCapitalizedFillGuard},
		"strategy_selection_events_state_guard":          {"strategy_selection_events", policyTriggerSQL["strategy_selection_events_state_guard"]},
		"execution_authority_events_state_guard":         {"execution_authority_events", policyTriggerSQL["execution_authority_events_state_guard"]},
		"paper_accounting_sessions_state_guard":          {"paper_accounting_sessions", strings.ToLower(strings.Join(strings.Fields(expectedPaperAccountingStateTrigger), " "))},
		"order_idempotency_legacy_paper_signal_guard":    {"order_idempotency", expectedLegacyAuthorizationGuard},
		"order_idempotency_capitalized_paper_guard":      {"order_idempotency", expectedCapitalizedOrderGuard},
		"paper_execution_authorizations_state_guard":     {"paper_execution_authorizations", expectedAuthorizationStateGuard},
		"paper_signal_events_capitalized_quantity_guard": {"paper_signal_events", expectedPaperSignalQuantityGuard},
		"paper_signal_events_state_guard":                {"paper_signal_events", strings.ToLower(strings.Join(strings.Fields(expectedPaperSignalStateTrigger), " "))},
		"paper_performance_events_state_guard":           {"paper_performance_events", expectedPerformanceStateTrigger},
		"paper_strategy_performance_events_state_guard":  {"paper_strategy_performance_events", expectedStrategyPerformanceStateTrigger},
		"paper_performance_policy_events_state_guard":    {"paper_performance_policy_events", policyTriggerSQL["paper_performance_policy_events_state_guard"]},
		"events_cash_void_guard":                         {"events", "create trigger events_cash_void_guard before insert on events when new.type = 'cash_void' begin select case when not exists ( select 1 from events target where target.account_id = new.account_id and target.source_event_id = new.corrects_source_event_id and target.type in ('deposit', 'withdrawal', 'dividend', 'fee', 'tax') and target.currency = new.currency and rtrim(target.occurred_at, 'z') <= rtrim(new.occurred_at, 'z') and new.amount = case when target.amount glob '-*' then substr(target.amount, 2) else '-' || target.amount end ) then raise(abort, 'cash void must exactly reverse an eligible event') end; end"},
	}
	for name, expected := range guardSQL {
		var definition string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=? AND tbl_name=?`, name, expected.table).Scan(&definition); err != nil {
			return fmt.Errorf("restore authority guard %s: %w", name, err)
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
		if normalized != expected.sql {
			return fmt.Errorf("restore authority guard %s does not match the required definition", name)
		}
	}
	return nil
}

func migrationTriggerDefinition(script, name string) (string, error) {
	prefix := "CREATE TRIGGER " + name
	start := strings.Index(script, prefix)
	if start < 0 {
		return "", errors.New("trigger is missing")
	}
	rest := script[start:]
	if next := strings.Index(rest[len(prefix):], "\nCREATE TRIGGER "); next >= 0 {
		rest = rest[:len(prefix)+next]
	}
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(rest), ";")), " ")), nil
}

func hasUniqueIndex(db *sql.DB, table string, expected []string, origin string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_index_list(?) WHERE "unique"=1 AND partial=0 AND origin=?`, table, origin)
	if err != nil {
		return false, err
	}
	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return false, err
		}
		indexes = append(indexes, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	for _, index := range indexes {
		columnRows, err := db.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
		if err != nil {
			return false, err
		}
		var columns []string
		for columnRows.Next() {
			var column sql.NullString
			if err := columnRows.Scan(&column); err != nil {
				columnRows.Close()
				return false, err
			}
			if column.Valid {
				columns = append(columns, column.String)
			}
		}
		if err := columnRows.Err(); err != nil {
			columnRows.Close()
			return false, err
		}
		if err := columnRows.Close(); err != nil {
			return false, err
		}
		if len(columns) == len(expected) {
			matches := true
			for i := range columns {
				matches = matches && columns[i] == expected[i]
			}
			if matches {
				return true, nil
			}
		}
	}
	return false, nil
}
