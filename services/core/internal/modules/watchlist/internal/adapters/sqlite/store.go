package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/codec"
	c "omni-folio/services/core/internal/modules/watchlist/contracts"
	"omni-folio/services/core/internal/modules/watchlist/internal/application"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
	"reflect"
	"strings"
	"time"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) observation.Scanner
}
type direct struct{ q *sql.Tx }

func (d direct) QueryRowContext(ctx context.Context, sql string, args ...any) observation.Scanner {
	return d.q.QueryRowContext(ctx, sql, args...)
}

type scoped struct{ q *observation.ReadHandle }

func (d scoped) QueryRowContext(ctx context.Context, sql string, args ...any) observation.Scanner {
	return d.q.QueryOne(ctx, sql, args...)
}

type Store struct {
	db     *sql.DB
	reader *observation.ReadResolver
	now    func() time.Time
}

func New(db *sql.DB, registry *observation.Registry, now func() time.Time) (*Store, error) {
	reader, err := registry.WatchlistReader([]observation.Statement{
		{SQL: `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name='watchlist_state'`, Arguments: 2, Objects: []string{"watchlist_state", "watchlist_state_no_delete", "watchlist_state_update_guard", "watchlist_state_no_insert"}, ObjectArguments: []int{0}},
		{SQL: `SELECT COUNT(*) FROM (
        SELECT name FROM sqlite_master WHERE tbl_name='watchlist_state' AND type IN ('index','trigger')
        UNION ALL SELECT name FROM sqlite_temp_master WHERE tbl_name='watchlist_state' OR name='watchlist_state'
    )`},
		{SQL: `SELECT COUNT(*) FROM watchlist_state`},
		{SQL: `SELECT revision,state_json,state_sha256 FROM watchlist_state WHERE singleton=1`},
	})
	if err != nil {
		return nil, err
	}
	return &Store{db, reader, now}, nil
}
func (s *Store) ReadState(ctx context.Context, ref storage.ObservationRef) (c.WatchlistState, error) {
	q, err := s.reader.Resolve(ref)
	if err != nil {
		return c.WatchlistState{}, err
	}
	state, _, err := readState(ctx, scoped{q})
	if err != nil {
		return c.WatchlistState{}, err
	}
	return *state, nil
}
func (s *Store) VerifyBackup(ctx context.Context, ref storage.ObservationRef) (c.RecoveryProof, error) {
	q, err := s.reader.Resolve(ref)
	if err != nil {
		return c.RecoveryProof{}, err
	}
	return proveWatchlistRecovery(ctx, scoped{q})
}
func requireWatchlistSchema(ctx context.Context, q queryer) error {
	script, err := migrations.Files.ReadFile("migrations/026_watchlist_state.sql")
	if err != nil {
		return err
	}
	for _, object := range []struct{ kind, name string }{{"TABLE", "watchlist_state"}, {"TRIGGER", "watchlist_state_no_delete"}, {"TRIGGER", "watchlist_state_update_guard"}, {"TRIGGER", "watchlist_state_no_insert"}} {
		expected := ""
		// shortcut: controlled migration DDL uses blank-line-separated blocks; use a SQL parser if that format changes.
		for _, block := range strings.Split(string(script), "\n\n") {
			definition := strings.Join(strings.Fields(block), " ")
			if strings.HasPrefix(definition, "CREATE "+object.kind+" "+object.name+" ") {
				expected = strings.TrimSuffix(definition, ";")
			}
		}
		var actual string
		if expected == "" || q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name='watchlist_state'`, object.name, strings.ToLower(object.kind)).Scan(&actual) != nil || strings.Join(strings.Fields(actual), " ") != expected {
			return errors.New("watchlist physical schema changed")
		}
	}
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
        SELECT name FROM sqlite_master WHERE tbl_name='watchlist_state' AND type IN ('index','trigger')
        UNION ALL SELECT name FROM sqlite_temp_master WHERE tbl_name='watchlist_state' OR name='watchlist_state'
    )`).Scan(&count); err != nil || count != 3 {
		return errors.New("watchlist schema inventory changed")
	}
	return nil
}

func readState(ctx context.Context, q queryer) (*c.WatchlistState, c.RecoveryProof, error) {
	var proof c.RecoveryProof
	if err := requireWatchlistSchema(ctx, q); err != nil {
		return nil, proof, err
	}
	var rows int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM watchlist_state`).Scan(&rows); err != nil || rows != 1 {
		return nil, proof, errors.New("watchlist singleton is missing")
	}
	var revision int64
	var raw, hash string
	if err := q.QueryRowContext(ctx, `SELECT revision,state_json,state_sha256 FROM watchlist_state WHERE singleton=1`).Scan(&revision, &raw, &hash); err != nil {
		return nil, proof, err
	}
	var state c.WatchlistState
	if len(raw) > c.BodyLimit || json.Unmarshal([]byte(raw), &state) != nil || state.Revision != revision || revision < 0 || revision > c.RevisionLimit {
		return nil, proof, errors.New("watchlist state is invalid")
	}
	entries, err := application.CanonicalEntries(state.Entries)
	if err != nil || !reflect.DeepEqual(entries, state.Entries) || (revision == 0 && (state.UpdatedAt != nil || len(entries) != 0)) ||
		(revision > 0 && (state.UpdatedAt == nil || !codec.CanonicalUTC(*state.UpdatedAt))) {
		return nil, proof, errors.New("watchlist state metadata is invalid")
	}
	canonical, sha, err := codec.JSONHash(state)
	if err != nil || string(canonical) != raw || sha != hash {
		return nil, proof, errors.New("watchlist state proof mismatch")
	}
	return &state, c.RecoveryProof{sha, revision, len(entries)}, nil
}

func proveWatchlistRecovery(ctx context.Context, q queryer) (c.RecoveryProof, error) {
	_, proof, err := readState(ctx, q)
	return proof, err
}

func (s *Store) Replace(ctx context.Context, change c.WatchlistChange) (*c.WatchlistState, error) {
	if _, err := application.CanonicalEntries(change.Entries); err != nil || change.ExpectedRevision < 0 || change.ExpectedRevision > c.RevisionLimit {
		return nil, c.ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	state, _, err := readState(ctx, direct{tx})
	if err != nil {
		return nil, err
	}
	next, changed, err := application.Transition(state, change, s.now)
	if err != nil {
		return nil, err
	}
	if !changed {
		return next, nil
	}
	raw, sha, err := codec.JSONHash(next)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE watchlist_state SET revision=?,state_json=?,state_sha256=? WHERE singleton=1 AND revision=?`, next.Revision, string(raw), sha, state.Revision)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return nil, errors.New("watchlist update did not affect one state")
	}
	stored, _, err := readState(ctx, direct{tx})
	if err != nil || !reflect.DeepEqual(stored, next) {
		return nil, errors.New("watchlist write verification failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}
