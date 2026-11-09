package sqlite

import (
	"context"
	"database/sql"
	"omni-folio/services/core/internal/storage/observation"
)

type rawQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type dbQuery struct{ rawQuerier }

func (q dbQuery) QueryRowContext(ctx context.Context, sql string, args ...any) observation.Scanner {
	return q.rawQuerier.QueryRowContext(ctx, sql, args...)
}
func newQueryer(q rawQuerier) Querier { return dbQuery{q} }

type observedQuery struct{ handle *observation.ReadHandle }

func (q observedQuery) QueryContext(ctx context.Context, sql string, args ...any) (*sql.Rows, error) {
	return q.handle.Query(ctx, sql, args...)
}
func (q observedQuery) QueryRowContext(ctx context.Context, sql string, args ...any) observation.Scanner {
	return q.handle.QueryOne(ctx, sql, args...)
}
