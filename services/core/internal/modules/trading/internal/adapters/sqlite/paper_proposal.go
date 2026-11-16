package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/adapters/files"

	"errors"
)

func (s *Store) AdmitPaperProposal(ctx context.Context, accountRef, selectionEventID string, raw []byte, barID string, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
	return s.processPaperProposal(ctx, accountRef, selectionEventID, raw, barID, fencingToken, true, nil)
}

func (s *Store) processPaperProposal(ctx context.Context, accountRef, selectionEventID string, raw []byte, barID string, fencingToken int64, admit bool, claim *paperRunnerClaim) (*PaperSignalEvent, *OrderState, error) {
	if s == nil || s.db == nil {
		return nil, nil, errors.New("paper proposal admission is not configured")
	}
	return s.business().ProcessPaperProposal(ctx, accountRef, selectionEventID, raw, barID, fencingToken, admit, claim)
}

var decodePaperProposal = files.DecodePaperProposal
