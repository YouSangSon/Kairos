package observation

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	storage "omni-folio/services/core/internal/storage/contracts"
)

var (
	ErrReference = errors.New("observation reference is unavailable or unauthorized")
	ErrStatement = errors.New("statement is not authorized for this observation")
)

type purpose uint8

const (
	valuation purpose = iota + 1
	watchlist
	ownerPriceImport
	backup
)

type Registry struct {
	db       *sql.DB
	now      func() time.Time
	mu       sync.Mutex
	sessions map[storage.ObservationRef]*session
}

type session struct {
	tx      *sql.Tx
	cutoff  time.Time
	purpose purpose
	active  bool
	mu      sync.Mutex
}

func New(db *sql.DB, now func() time.Time) *Registry {
	return &Registry{db: db, now: now, sessions: make(map[storage.ObservationRef]*session)}
}

func (r *Registry) ReadValuation(ctx context.Context, work func(storage.ObservationRef, time.Time) error) error {
	return r.observe(ctx, valuation, true, work)
}

func (r *Registry) ReadWatchlist(ctx context.Context, work func(storage.ObservationRef, time.Time) error) error {
	return r.observe(ctx, watchlist, true, work)
}

func (r *Registry) ImportOwnerPrice(ctx context.Context, work func(storage.ObservationRef, time.Time) error) error {
	return r.observe(ctx, ownerPriceImport, false, work)
}

func (r *Registry) VerifyBackup(ctx context.Context, work func(storage.ObservationRef, time.Time) error) error {
	return r.observe(ctx, backup, true, work)
}

func (r *Registry) observe(ctx context.Context, use purpose, readOnly bool, work func(storage.ObservationRef, time.Time) error) error {
	if r == nil || r.db == nil || r.now == nil || work == nil {
		return ErrReference
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	ref := storage.NewObservationRef()
	s := &session{tx: tx, cutoff: r.now().UTC(), purpose: use, active: true}
	r.mu.Lock()
	r.sessions[ref] = s
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.sessions, ref)
		r.mu.Unlock()
		s.mu.Lock()
		s.active = false
		_ = tx.Rollback()
		s.mu.Unlock()
	}()
	if err := work(ref, s.cutoff); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Registry) find(ref storage.ObservationRef) (*session, error) {
	if r == nil {
		return nil, ErrReference
	}
	r.mu.Lock()
	s := r.sessions[ref]
	r.mu.Unlock()
	if s == nil {
		return nil, ErrReference
	}
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if !active {
		return nil, ErrReference
	}
	return s, nil
}
