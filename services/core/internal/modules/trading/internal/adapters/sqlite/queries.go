package sqlite

import (
	"context"
	"database/sql"
)

type rowset interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}
type database struct{ *sql.DB }
type transaction struct{ *sql.Tx }

func (d *database) QueryContext(ctx context.Context, q string, args ...any) (rowset, error) {
	return d.DB.QueryContext(ctx, q, args...)
}
func (d *database) QueryRowContext(ctx context.Context, q string, args ...any) rowScanner {
	return d.DB.QueryRowContext(ctx, q, args...)
}
func (d *database) BeginTx(ctx context.Context, options *sql.TxOptions) (*transaction, error) {
	tx, err := d.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &transaction{tx}, nil
}
func (t *transaction) QueryContext(ctx context.Context, q string, args ...any) (rowset, error) {
	return t.Tx.QueryContext(ctx, q, args...)
}
func (t *transaction) QueryRowContext(ctx context.Context, q string, args ...any) rowScanner {
	return t.Tx.QueryRowContext(ctx, q, args...)
}
