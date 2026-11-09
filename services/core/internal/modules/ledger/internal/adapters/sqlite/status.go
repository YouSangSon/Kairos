package sqlite

import (
	"context"
	"database/sql"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
)

func (s *Service) Status(ctx context.Context) (c.Status, error) {
	var rev int64
	var verified sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT revision, last_verified_at FROM ledger_meta WHERE singleton=1`).Scan(&rev, &verified); err != nil {
		return c.Status{}, err
	}
	var last *string
	if verified.Valid {
		last = &verified.String
	}
	return c.Status{LedgerRevision: revision(rev), LastVerifiedAt: last}, nil
}
