package ownership

import (
	"sort"
	"strings"
)

const ControlTable = "schema_migrations"

var owners = map[string]string{
	"events": "ledger", "ledger_meta": "ledger", "previews": "ledger", "receipts": "ledger",
	"ledger_replay_state": "ledger", "ledger_replay_transition": "ledger",
	"broker_snapshots": "ledger", "broker_snapshot_reconciliations": "ledger",
	"fx_observations": "marketdata", "security_price_observations": "marketdata",
	"instrument_listing_events": "marketdata", "owner_fx_series_versions": "marketdata",
	"owner_price_series_versions": "marketdata", "benchmark_series_versions": "marketdata",
	"watchlist_state":   "watchlist",
	"order_idempotency": "trading", "order_events": "trading", "execution_authority_events": "trading",
	"risk_reservations": "trading", "strategy_research_evidence": "trading", "strategy_selection_events": "trading",
	"paper_evaluation_events": "trading", "paper_accounting_sessions": "trading",
	"paper_market_bar_observations": "trading", "paper_signal_events": "trading",
	"paper_execution_authorizations": "trading", "paper_performance_events": "trading",
	"paper_strategy_performance_events": "trading", "paper_performance_policy_events": "trading",
	"paper_runner_leases": "trading", "paper_promotion_claims": "trading", "paper_promotion_decisions": "trading",
	ControlTable: "maintenance",
}

func Canonical(table string) (string, bool) {
	switch table {
	case "events_v5", "events_v8", "events_v9":
		table = "events"
	case "order_idempotency_v7":
		table = "order_idempotency"
	}
	if len(table) > 4 && table[len(table)-4:] == "_new" {
		table = table[:len(table)-4]
	}
	_, ok := owners[table]
	return table, ok
}

func Owner(table string) (string, bool) {
	table, ok := Canonical(table)
	if !ok {
		return "", false
	}
	return owners[table], true
}

func ObjectOwner(name string) (string, bool) {
	if owner, ok := Owner(name); ok {
		return owner, true
	}
	aliases := map[string]string{
		"execution_authority_latest_idx": "execution_authority_events",
		"owner_price_series_latest_idx":  "owner_price_series_versions",
		"owner_fx_series_latest_idx":     "owner_fx_series_versions",
		"benchmark_series_latest_idx":    "benchmark_series_versions",
	}
	if table, ok := aliases[name]; ok {
		return Owner(table)
	}
	for table, owner := range owners {
		if strings.HasPrefix(name, table+"_") {
			return owner, true
		}
	}
	return "", false
}

func Tables(owner string) []string {
	var result []string
	for table, candidate := range owners {
		if candidate == owner {
			result = append(result, table)
		}
	}
	sort.Strings(result)
	return result
}

func IsCatalog(name string) bool { return name == "sqlite_master" || name == "sqlite_temp_master" }
