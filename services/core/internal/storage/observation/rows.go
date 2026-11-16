package observation

import (
	"context"
	"database/sql"
)

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

type scopedRows struct {
	session *session
	rows    *sql.Rows
}

func (r *scopedRows) Next() bool {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.session.active && r.rows.Next()
}

func (r *scopedRows) Scan(values ...any) error {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.session.active {
		return ErrReference
	}
	return r.rows.Scan(values...)
}

func (r *scopedRows) Err() error {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.session.active {
		return ErrReference
	}
	return r.rows.Err()
}

func (r *scopedRows) Close() error {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	delete(r.session.rows, r)
	return r.rows.Close()
}

type scopedScanner struct {
	handle *ReadHandle
	ctx    context.Context
	query  string
	args   []any
}

func (r *scopedScanner) Scan(values ...any) error {
	r.handle.session.mu.Lock()
	defer r.handle.session.mu.Unlock()
	if !r.handle.session.active {
		return ErrReference
	}
	return r.handle.session.tx.QueryRowContext(r.ctx, r.query, r.args...).Scan(values...)
}
