package sqlite

import (
	"context"
	"errors"

	"omni-folio/services/core/internal/modules/trading/contracts"
)

type PaperInputStatus = contracts.PaperInputStatus

func (s *Store) PaperInputStatus(ctx context.Context, account, selection string, raw []byte) (*PaperInputStatus, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper input status identity is invalid")
	}
	return s.business().PaperInputStatus(ctx, account, selection, raw)
}
