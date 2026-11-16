package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
	"sort"
	"strings"
	"time"
)

var migrationNames = [...]string{"001_init.sql", "002_orders.sql", "003_broker_snapshots.sql", "004_execution_authority.sql", "005_ledger_events.sql", "006_strategy_registry.sql", "007_paper_orders.sql", "008_cash_void.sql", "009_fx_exchange.sql", "010_fx_observations.sql", "011_security_price_observations.sql", "012_kiwoom_security_price_observations.sql", "013_instrument_listing_events.sql", "014_paper_evaluation_events.sql", "015_paper_accounting_sessions.sql", "016_paper_market_signals.sql", "017_paper_execution_authorizations.sql", "018_paper_performance_events.sql", "019_paper_strategy_performance_events.sql", "020_paper_performance_policy_events.sql", "021_paper_runner_leases.sql", "022_ledger_replay_policy.sql", "023_benchmark_series_versions.sql", "024_owner_fx_series_versions.sql", "025_owner_price_series_versions.sql", "026_watchlist_state.sql", "027_paper_promotion.sql"}

func (s *Store) Migrate(ctx context.Context) error {
	db := s.db
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&exists); err != nil {
		return err
	}
	current := 0
	if exists != 0 {
		var first, count int
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`).Scan(&first, &current, &count); err != nil {
			return err
		}
		if first != 1 || current > latestSchema || count != current {
			return fmt.Errorf("unsupported schema version %d", current)
		}
	}

	for version := current + 1; version <= latestSchema; version++ {
		script, err := migrations.Files.ReadFile("migrations/" + migrationNames[version-1])
		if err != nil {
			return err
		}
		if err := s.migrateVersion(ctx, version, script); err != nil {
			return err
		}

	}
	return nil
}

func (s *Store) migrateVersion(ctx context.Context, version int, script []byte) error {
	db := s.db
	disableForeignKeys := version == 7 || version == 20 || version == 27
	checkForeignKeys := version >= 7
	if disableForeignKeys {
		if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
			return err
		}
	}
	tx, proof, err := s.observations.MigrationStarter().Begin(ctx)
	if err != nil {
		if disableForeignKeys {
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
		}
		return err
	}
	defer func() {
		_ = tx.Rollback()
		if disableForeignKeys {
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
		}
	}()
	var v20Before migration20Snapshot
	var v27Before migration27Snapshot
	if version == 20 {
		v20Before, err = s.captureMigration20Snapshot(ctx, tx, proof)
		if err != nil {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			return fmt.Errorf("migration 20 pre-rebuild proof: %w", err)
		}
		if err := dropMigration20DependentTriggers(tx, v20Before.DependentTriggers); err != nil {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			return fmt.Errorf("migration 20 rebuild preparation: %w", err)
		}
	}
	if version == 27 {
		v27Before, err = s.captureMigration27Snapshot(ctx, tx, proof, false)
		if err == nil {
			err = dropMigration20DependentTriggers(tx, v27Before.DependentTriggers)
		}
		if err != nil {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			return fmt.Errorf("migration 27 pre-rebuild proof: %w", err)
		}
	}
	if _, err := tx.Exec(string(script)); err != nil {
		tx.Rollback()
		if disableForeignKeys {
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
		}
		return err
	}
	if version == 20 {
		if err := restoreMigration20DependentTriggers(tx, v20Before.DependentTriggers); err != nil {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			return fmt.Errorf("migration 20 trigger restoration: %w", err)
		}
		if err := s.verifyMigration20Snapshot(ctx, tx, proof, v20Before); err != nil {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			return fmt.Errorf("migration 20 rebuild proof: %w", err)
		}
	}
	if version == 27 {
		for _, index := range v27Before.SelectionIndexes {
			if _, err = tx.Exec(index.SQL); err != nil {
				break
			}
		}
		if err == nil {
			err = restoreMigration20DependentTriggers(tx, v27Before.DependentTriggers)
		}
		if err == nil {
			err = s.verifyMigration27Snapshot(ctx, tx, proof, v27Before)
		}
		if err != nil {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			return fmt.Errorf("migration 27 rebuild proof: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		if disableForeignKeys {
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
		}
		return err
	}
	if checkForeignKeys {
		var violations int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
			tx.Rollback()
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
			if err != nil {
				return err
			}
			return fmt.Errorf("migration %d foreign-key check failed: violations=%d", version, violations)
		}
	}
	if err := tx.Commit(); err != nil {
		if disableForeignKeys {
			_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
		}
		return err
	}
	if disableForeignKeys {
		if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
			return err
		}
		var foreignKeysEnabled, violations int
		if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeysEnabled); err != nil || foreignKeysEnabled != 1 {
			if err != nil {
				return err
			}
			return fmt.Errorf("migration %d foreign keys are not enabled", version)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
			if err != nil {
				return err
			}
			return fmt.Errorf("migration %d post-enable foreign-key check failed: violations=%d", version, violations)
		}
	}
	return nil
}

type migration20Trigger struct {
	Name, Table, SQL string
}

func dropMigration20DependentTriggers(tx *sql.Tx, triggers []migration20Trigger) error {
	for _, trigger := range triggers {
		if _, err := tx.Exec(`DROP TRIGGER ` + quoteSQLiteIdentifier(trigger.Name)); err != nil {
			return err
		}
	}
	return nil
}

func restoreMigration20DependentTriggers(tx *sql.Tx, triggers []migration20Trigger) error {
	for _, trigger := range triggers {
		if _, err := tx.Exec(trigger.SQL); err != nil {
			return err
		}
	}
	return nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

type migration20JournalShape = trading.JournalShape

type migration20ForeignKey struct {
	Child, Parent, From, To, OnUpdate, OnDelete, Match string
}

const migration20CanonicalV19ForeignKeyInventorySHA256 = "665f412a571d311c990d15f579608f89d91cbd8376b97dc1fdd0dd2537e41cbd"

type migration20Snapshot struct {
	AuthoritySHA      string
	AuthorityCount    int
	AuthorityShape    migration20JournalShape
	Strategy          trading.StrategyRecovery
	StrategyShape     migration20JournalShape
	ForeignKeys       []migration20ForeignKey
	DependentTriggers []migration20Trigger
}

func (s *Store) captureMigration20Snapshot(ctx context.Context, q orderQuerier, proof *observation.MigrationProof) (migration20Snapshot, error) {
	return s.captureMigration20SnapshotWithForeignKeyPreflight(ctx, q, proof, true)
}

func (s *Store) captureMigration20PostRebuildSnapshot(ctx context.Context, q orderQuerier, proof *observation.MigrationProof) (migration20Snapshot, error) {
	return s.captureMigration20SnapshotWithForeignKeyPreflight(ctx, q, proof, false)
}

func (s *Store) captureMigration20SnapshotWithForeignKeyPreflight(ctx context.Context, q orderQuerier, proof *observation.MigrationProof, requireCanonicalV19ForeignKeys bool) (migration20Snapshot, error) {
	var schemaVersion int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		return migration20Snapshot{}, err
	}
	if schemaVersion != 19 {
		return migration20Snapshot{}, fmt.Errorf("expected schema version 19, got %d", schemaVersion)
	}
	facts, err := s.readMigrationFacts(ctx, proof, trading.MigrationQuery{Schema: 19, PostRebuild: !requireCanonicalV19ForeignKeys})
	if err != nil {
		return migration20Snapshot{}, err
	}
	authoritySHA, authorityCount := facts.AuthoritySHA, facts.AuthorityCount
	strategy := facts.Strategy
	authorityShape, strategyShape := facts.AuthorityShape, facts.StrategyShape
	foreignKeys, err := migration20ForeignKeyInventory(ctx, q)
	if err != nil {
		return migration20Snapshot{}, err
	}
	if requireCanonicalV19ForeignKeys && migration20ForeignKeyInventorySHA256(foreignKeys) != migration20CanonicalV19ForeignKeyInventorySHA256 {
		return migration20Snapshot{}, errors.New("v19 foreign-key inventory does not match canonical migrations")
	}
	dependentTriggers, err := migration20DependentTriggerInventory(ctx, q)
	if err != nil {
		return migration20Snapshot{}, err
	}
	return migration20Snapshot{
		AuthoritySHA: authoritySHA, AuthorityCount: authorityCount, AuthorityShape: authorityShape,
		Strategy: strategy, StrategyShape: strategyShape, ForeignKeys: foreignKeys, DependentTriggers: dependentTriggers,
	}, nil
}

func (s *Store) verifyMigration20Snapshot(ctx context.Context, q orderQuerier, proof *observation.MigrationProof, before migration20Snapshot) error {
	var schemaVersion int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		return err
	}
	if schemaVersion != 19 {
		return fmt.Errorf("schema history changed before migration record: %d", schemaVersion)
	}
	after, err := s.captureMigration20PostRebuildSnapshot(ctx, q, proof)
	if err != nil {
		return err
	}
	if before.AuthoritySHA != after.AuthoritySHA || before.AuthorityCount != after.AuthorityCount || before.AuthorityShape != after.AuthorityShape ||
		before.Strategy.SHA256 != after.Strategy.SHA256 || before.Strategy.Evidence != after.Strategy.Evidence ||
		before.Strategy.Events != after.Strategy.Events || before.Strategy.Evaluations != after.Strategy.Evaluations ||
		before.Strategy.SelectedResultSHA256 != after.Strategy.SelectedResultSHA256 || before.Strategy.CurrentEventID != after.Strategy.CurrentEventID ||
		before.StrategyShape != after.StrategyShape {
		return errors.New("authority or strategy journal was not preserved exactly")
	}
	expectedForeignKeys, err := migration20ExpectedForeignKeys(before.ForeignKeys)
	if err != nil {
		return err
	}
	if !sameMigration20ForeignKeys(expectedForeignKeys, after.ForeignKeys) {
		return errors.New("foreign-key inventory was not preserved exactly")
	}
	if !sameMigration20Triggers(before.DependentTriggers, after.DependentTriggers) {
		return errors.New("dependent trigger inventory was not preserved exactly")
	}
	if err := verifyMigration20TemporaryNames(ctx, q, after.ForeignKeys); err != nil {
		return err
	}
	return nil
}

func migration20DependentTriggerInventory(ctx context.Context, q orderQuerier) ([]migration20Trigger, error) {
	rows, err := q.QueryContext(ctx, `SELECT name,tbl_name,sql FROM sqlite_master
		WHERE type='trigger' AND tbl_name NOT IN ('execution_authority_events','strategy_selection_events','paper_performance_policy_events')
		AND (instr(sql, 'execution_authority_events') != 0 OR instr(sql, 'strategy_selection_events') != 0)
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var triggers []migration20Trigger
	for rows.Next() {
		var trigger migration20Trigger
		if err := rows.Scan(&trigger.Name, &trigger.Table, &trigger.SQL); err != nil {
			rows.Close()
			return nil, err
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return triggers, nil
}

func sameMigration20Triggers(left, right []migration20Trigger) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func migration20ForeignKeyInventory(ctx context.Context, q orderQuerier) ([]migration20ForeignKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var inventory []migration20ForeignKey
	for _, table := range tables {
		foreignRows, err := q.QueryContext(ctx, `SELECT "table","from","to","on_update","on_delete","match" FROM pragma_foreign_key_list(?)`, table)
		if err != nil {
			return nil, err
		}
		for foreignRows.Next() {
			var item migration20ForeignKey
			item.Child = table
			if err := foreignRows.Scan(&item.Parent, &item.From, &item.To, &item.OnUpdate, &item.OnDelete, &item.Match); err != nil {
				foreignRows.Close()
				return nil, err
			}
			inventory = append(inventory, item)
		}
		if err := foreignRows.Err(); err != nil {
			foreignRows.Close()
			return nil, err
		}
		if err := foreignRows.Close(); err != nil {
			return nil, err
		}
	}
	sort.Slice(inventory, func(i, j int) bool {
		return migration20ForeignKeyKey(inventory[i]) < migration20ForeignKeyKey(inventory[j])
	})
	return inventory, nil
}

func migration20ExpectedForeignKeys(before []migration20ForeignKey) ([]migration20ForeignKey, error) {
	if migration20ForeignKeyInventorySHA256(before) != migration20CanonicalV19ForeignKeyInventorySHA256 {
		return nil, errors.New("v19 foreign-key inventory does not match canonical migrations")
	}
	expected := append([]migration20ForeignKey(nil), before...)
	expected = append(expected,
		migration20ForeignKey{Child: "execution_authority_events", Parent: "paper_performance_policy_events", From: "paper_performance_policy_event_id", To: "policy_event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "strategy_selection_events", Parent: "paper_performance_policy_events", From: "paper_performance_policy_event_id", To: "policy_event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
	)
	expected = append(expected, migration20PolicyForeignKeys()...)
	sort.Slice(expected, func(i, j int) bool {
		return migration20ForeignKeyKey(expected[i]) < migration20ForeignKeyKey(expected[j])
	})
	return expected, nil
}

func migration20PolicyForeignKeys() []migration20ForeignKey {
	const child = "paper_performance_policy_events"
	return []migration20ForeignKey{
		{Child: child, Parent: "paper_accounting_sessions", From: "paper_accounting_session_id", To: "session_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		{Child: child, Parent: "strategy_selection_events", From: "strategy_selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		{Child: child, Parent: "strategy_research_evidence", From: "selected_strategy_result_ref", To: "result_sha256", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		{Child: child, Parent: "paper_strategy_performance_events", From: "strategy_performance_id", To: "strategy_performance_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		{Child: child, Parent: "paper_performance_events", From: "baseline_performance_id", To: "performance_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		{Child: child, Parent: "paper_performance_events", From: "latest_performance_id", To: "performance_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		{Child: child, Parent: "strategy_selection_events", From: "rollback_selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
	}
}

func sameMigration20ForeignKeys(left, right []migration20ForeignKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func migration20ForeignKeyKey(item migration20ForeignKey) string {
	return strings.Join([]string{item.Child, item.Parent, item.From, item.To, item.OnUpdate, item.OnDelete, item.Match}, "\x00")
}

func migration20ForeignKeyInventorySHA256(inventory []migration20ForeignKey) string {
	keys := make([]string, 0, len(inventory))
	for _, foreignKey := range inventory {
		keys = append(keys, migration20ForeignKeyKey(foreignKey))
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func verifyMigration20TemporaryNames(ctx context.Context, q orderQuerier, foreignKeys []migration20ForeignKey) error {
	var temporaryTables int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND (name GLOB '*_new' OR name GLOB '*_old')`).Scan(&temporaryTables); err != nil {
		return err
	}
	if temporaryTables != 0 {
		return errors.New("migration temporary table remains")
	}
	for _, foreignKey := range foreignKeys {
		if strings.HasSuffix(foreignKey.Child, "_new") || strings.HasSuffix(foreignKey.Child, "_old") ||
			strings.HasSuffix(foreignKey.Parent, "_new") || strings.HasSuffix(foreignKey.Parent, "_old") {
			return errors.New("migration foreign key names a temporary table")
		}
	}
	return nil
}

type migration27SchemaObject struct {
	Type, Name, Table, SQL string
}

type migration27Snapshot struct {
	Orders            trading.OrderRecovery
	Strategy          trading.StrategyRecovery
	SelectionShape    migration20JournalShape
	ForeignKeys       []migration20ForeignKey
	SchemaObjects     []migration27SchemaObject
	SelectionIndexes  []migration27SchemaObject
	DependentTriggers []migration20Trigger
}

func (s *Store) captureMigration27Snapshot(ctx context.Context, q orderQuerier, proof *observation.MigrationProof, postRebuild bool) (migration27Snapshot, error) {
	if err := requireSchemaVersionContext(ctx, q, 26); err != nil {
		return migration27Snapshot{}, err
	}
	facts, err := s.readMigrationFacts(ctx, proof, trading.MigrationQuery{Schema: 26, PostRebuild: postRebuild})
	if err != nil {
		return migration27Snapshot{}, err
	}
	orders, strategy, shape := facts.Orders, facts.Strategy, facts.SelectionShape
	foreignKeys, err := migration20ForeignKeyInventory(ctx, q)
	if err != nil {
		return migration27Snapshot{}, err
	}
	objects, indexes, triggers, err := migration27SchemaInventory(ctx, q)
	if err != nil {
		return migration27Snapshot{}, err
	}
	return migration27Snapshot{
		Orders: orders, Strategy: strategy, SelectionShape: shape, ForeignKeys: foreignKeys,
		SchemaObjects: objects, SelectionIndexes: indexes, DependentTriggers: triggers,
	}, nil
}

func migration27SchemaInventory(ctx context.Context, q orderQuerier) ([]migration27SchemaObject, []migration27SchemaObject, []migration20Trigger, error) {
	rows, err := q.QueryContext(ctx, `SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%' AND tbl_name NOT IN ('paper_promotion_claims','paper_promotion_decisions') ORDER BY type,name`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	var objects, indexes []migration27SchemaObject
	var triggers []migration20Trigger
	for rows.Next() {
		var object migration27SchemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, nil, nil, err
		}
		if object.Table == "strategy_selection_events" {
			if object.Type == "trigger" {
				switch object.Name {
				case "strategy_selection_events_no_update", "strategy_selection_events_no_delete", "strategy_selection_events_state_guard":
				default:
					return nil, nil, nil, fmt.Errorf("unsupported strategy selection trigger %q", object.Name)
				}
			}
			if object.Type == "index" && object.SQL != "" {
				indexes = append(indexes, object)
			}
			continue
		}
		objects = append(objects, object)
		if object.Type == "trigger" && strings.Contains(object.SQL, "strategy_selection_events") {
			triggers = append(triggers, migration20Trigger{Name: object.Name, Table: object.Table, SQL: object.SQL})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	return objects, indexes, triggers, nil
}

func sameMigration27SchemaObjects(left, right []migration27SchemaObject) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Store) verifyMigration27Snapshot(ctx context.Context, q orderQuerier, proof *observation.MigrationProof, before migration27Snapshot) error {
	after, err := s.captureMigration27Snapshot(ctx, q, proof, true)
	if err != nil {
		return err
	}
	if before.Orders != after.Orders || !sameStrategyRegistryProof(before.Strategy, after.Strategy) || before.SelectionShape != after.SelectionShape {
		return errors.New("order or strategy journal was not preserved exactly")
	}
	if !sameMigration27SchemaObjects(before.SchemaObjects, after.SchemaObjects) ||
		!sameMigration27SchemaObjects(before.SelectionIndexes, after.SelectionIndexes) || !sameMigration20Triggers(before.DependentTriggers, after.DependentTriggers) {
		return errors.New("existing schema or dependent trigger inventory was not preserved exactly")
	}
	expected := append([]migration20ForeignKey(nil), before.ForeignKeys...)
	expected = append(expected,
		migration20ForeignKey{Child: "strategy_selection_events", Parent: "paper_promotion_decisions", From: "paper_promotion_attempt_id", To: "attempt_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "paper_promotion_decisions", Parent: "paper_promotion_claims", From: "attempt_id", To: "attempt_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "paper_promotion_decisions", Parent: "strategy_selection_events", From: "selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
	)
	sort.Slice(expected, func(i, j int) bool {
		return migration20ForeignKeyKey(expected[i]) < migration20ForeignKeyKey(expected[j])
	})
	if !sameMigration20ForeignKeys(expected, after.ForeignKeys) {
		return errors.New("foreign-key inventory was not preserved exactly")
	}
	facts, err := s.readMigrationFacts(ctx, proof, trading.MigrationQuery{Schema: 26, PostRebuild: true})
	if err != nil {
		return err
	}
	addedRows := facts.AddedPromotionRows
	if addedRows != 0 {
		return errors.New("migration introduced paper promotion history")
	}
	return verifyMigration20TemporaryNames(ctx, q, after.ForeignKeys)
}
