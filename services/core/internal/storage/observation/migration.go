package observation

import (
	"context"
	"database/sql"
	"sync"
	"time"

	storage "omni-folio/services/core/internal/storage/contracts"
)

type MigrationStarter struct{ registry *Registry }
type MigrationProof struct {
	registry *Registry
	tx       *sql.Tx
	verify   sync.Mutex
}

func (r *Registry) MigrationStarter() *MigrationStarter { return &MigrationStarter{registry: r} }
func (s *MigrationStarter) Begin(ctx context.Context) (*sql.Tx, *MigrationProof, error) {
	if s == nil || s.registry == nil || s.registry.db == nil || s.registry.now == nil {
		return nil, nil, ErrReference
	}
	tx, err := s.registry.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	return tx, &MigrationProof{registry: s.registry, tx: tx}, nil
}

func (p *MigrationProof) Verify(ctx context.Context, work func(storage.ObservationRef, time.Time) error) error {
	if p == nil || p.registry == nil || p.tx == nil || work == nil {
		return ErrReference
	}
	p.verify.Lock()
	defer p.verify.Unlock()
	var active int
	if err := p.tx.QueryRowContext(ctx, `SELECT 1`).Scan(&active); err != nil {
		return err
	}
	ref := storage.NewObservationRef()
	scope := &session{tx: p.tx, cutoff: p.registry.now().UTC(), purpose: migration, active: true, rows: make(map[*scopedRows]struct{})}
	p.registry.mu.Lock()
	p.registry.sessions[ref] = scope
	p.registry.mu.Unlock()
	defer func() {
		p.registry.mu.Lock()
		delete(p.registry.sessions, ref)
		p.registry.mu.Unlock()
		scope.mu.Lock()
		scope.active = false
		for rows := range scope.rows {
			_ = rows.rows.Close()
			delete(scope.rows, rows)
		}
		scope.mu.Unlock()
	}()
	if err := work(ref, scope.cutoff); err != nil {
		return err
	}
	return ctx.Err()
}
