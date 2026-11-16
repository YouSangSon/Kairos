package sqlite

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func openExistingDB(path string) (*sql.DB, error) { return openDB(path) }
func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
func newService(db *sql.DB, now func() time.Time, id func(string) string) *Store {
	return New(db, now, id, randomID("execution_owner"), randomID("paper_runner_owner"))
}
func fixturePath(name string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("fixture source path unavailable")
	}
	return filepath.Join(filepath.Dir(file), "../../../../../../../../contracts/fixtures", name)
}

// Only fresh fixture databases use this bootstrap; migration behavior remains covered at the composition root.
func migrate(db *sql.DB) error {
	files := []string{
		"001_init.sql", "002_orders.sql", "003_broker_snapshots.sql", "004_execution_authority.sql", "005_ledger_events.sql", "006_strategy_registry.sql", "007_paper_orders.sql", "008_cash_void.sql", "009_fx_exchange.sql", "010_fx_observations.sql", "011_security_price_observations.sql", "012_kiwoom_security_price_observations.sql", "013_instrument_listing_events.sql", "014_paper_evaluation_events.sql", "015_paper_accounting_sessions.sql", "016_paper_market_signals.sql", "017_paper_execution_authorizations.sql", "018_paper_performance_events.sql", "019_paper_strategy_performance_events.sql", "020_paper_performance_policy_events.sql", "021_paper_runner_leases.sql", "022_ledger_replay_policy.sql", "023_benchmark_series_versions.sql", "024_owner_fx_series_versions.sql", "025_owner_price_series_versions.sql", "026_watchlist_state.sql", "027_paper_promotion.sql",
	}
	expectedTriggers := map[string]bool{}
	triggerStatements := regexp.MustCompile(`(?m)^(CREATE|DROP) TRIGGER (?:IF EXISTS )?([a-zA-Z0-9_]+)`)
	for i, name := range files {
		script, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		for _, statement := range triggerStatements.FindAllStringSubmatch(string(script), -1) {
			if statement[1] == "CREATE" {
				expectedTriggers[statement[2]] = true
			} else {
				delete(expectedTriggers, statement[2])
			}
		}
		rebuild := i+1 == 7 || i+1 == 20 || i+1 == 27
		if rebuild {
			if _, err = db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
				return err
			}
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		err = func() error {
			defer tx.Rollback()
			var dependent []string
			var indexes []string
			if i+1 == 27 {
				rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='strategy_selection_events' AND sql IS NOT NULL ORDER BY name`)
				if err != nil {
					return err
				}
				for rows.Next() {
					var body string
					if err := rows.Scan(&body); err != nil {
						rows.Close()
						return err
					}
					indexes = append(indexes, body)
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					return err
				}
				if err := rows.Close(); err != nil {
					return err
				}
			}
			if i+1 == 20 || i+1 == 27 {
				rows, err := tx.Query(`SELECT name,sql FROM sqlite_master WHERE type='trigger' ORDER BY name`)
				if err != nil {
					return err
				}
				var names []string
				for rows.Next() {
					var trigger, body string
					if err := rows.Scan(&trigger, &body); err != nil {
						rows.Close()
						return err
					}
					if strings.Contains(body, "strategy_selection_events") || (i+1 == 20 && strings.Contains(body, "execution_authority_events")) {
						if !strings.Contains(string(script), "DROP TRIGGER "+trigger+";") {
							names = append(names, trigger)
							dependent = append(dependent, body)
						}
					}
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					return err
				}
				if err := rows.Close(); err != nil {
					return err
				}
				for _, trigger := range names {
					if _, err := tx.Exec(`DROP TRIGGER "` + trigger + `"`); err != nil {
						return err
					}
				}
			}
			if _, err := tx.Exec(string(script)); err != nil {
				return fmt.Errorf("fixture migration %s: %w", name, err)
			}
			for _, body := range indexes {
				if _, err := tx.Exec(body); err != nil {
					return err
				}
			}
			for _, body := range dependent {
				if _, err := tx.Exec(body); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)`, i+1, "2026-01-10T15:00:00Z"); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if rebuild {
			_, restoreErr := db.Exec(`PRAGMA foreign_keys=ON`)
			if err == nil {
				err = restoreErr
			}
		}
		if err != nil {
			return err
		}
	}
	var version, count, enabled int
	if err := db.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_migrations`).Scan(&version, &count); err != nil {
		return err
	}
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return err
	}
	if version != latestSchema || count != latestSchema || enabled != 1 {
		return fmt.Errorf("fixture schema=%d count=%d foreign_keys=%d", version, count, enabled)
	}
	triggers, err := db.Query(`SELECT name FROM sqlite_master WHERE type='trigger'`)
	if err != nil {
		return err
	}
	for triggers.Next() {
		var name string
		if err := triggers.Scan(&name); err != nil {
			triggers.Close()
			return err
		}
		if !expectedTriggers[name] {
			triggers.Close()
			return fmt.Errorf("unexpected fixture trigger %s", name)
		}
		delete(expectedTriggers, name)
	}
	if err := triggers.Err(); err != nil {
		triggers.Close()
		return err
	}
	if err := triggers.Close(); err != nil {
		return err
	}
	if len(expectedTriggers) > 0 {
		return fmt.Errorf("fixture triggers missing: %v", expectedTriggers)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("fixture foreign key violation")
	}
	return rows.Err()
}

func testService(t *testing.T, times []time.Time, ids map[string][]string) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	clockIndex := 0
	clock := func() time.Time {
		if len(times) == 0 {
			return mustTime("2026-01-10T15:01:00Z").Add(time.Duration(clockIndex) * time.Millisecond)
		}
		if clockIndex >= len(times) {
			return times[len(times)-1].Add(time.Duration(clockIndex-len(times)+1) * time.Millisecond)
		}
		v := times[clockIndex]
		clockIndex++
		return v
	}
	counters := map[string]int{}
	id := func(prefix string) string {
		i := counters[prefix]
		counters[prefix]++
		if i < len(ids[prefix]) {
			return ids[prefix][i]
		}
		return prefix + "_test_" + time.Unix(0, int64(i)).Format("150405.000000000")
	}
	return newService(db, clock, id), path
}
