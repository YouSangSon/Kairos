package ledger

import (
	"context"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/ledgerdomain"
)

func Snapshot(ctx context.Context, q Reader) (*ledgerdomain.PortfolioSnapshot, error) {
	origin, err := q.ReplayOrigin(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger replay state: %w", err)
	}
	if origin.Policy != ledgerdomain.FIFOCostBasisPolicy {
		return nil, errors.New("ledger replay upgrade required; preview and activate the corrected replay before normal use")
	}
	events, err := q.EventRange(ctx)
	if err != nil {
		return nil, err
	}
	if origin.Revision < 0 || events.Count != origin.Revision || events.Last != origin.Revision || (events.Count > 0 && events.First != 1) {
		return nil, errors.New("ledger replay revision does not match contiguous events")
	}
	return SnapshotAt(ctx, q, ledgerdomain.FIFOCostBasisPolicy, -1)
}

func SnapshotAt(ctx context.Context, q Reader, policy string, throughRevision int64) (*ledgerdomain.PortfolioSnapshot, error) {
	if policy != ledgerdomain.FIFOCostBasisPolicy && policy != ledgerdomain.LegacyFIFOCostBasisPolicy {
		return nil, errors.New("unsupported ledger replay policy")
	}
	meta, err := q.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	rev, recorded := meta.Revision, meta.RecordedAt
	if throughRevision >= 0 {
		if throughRevision > rev {
			return nil, errors.New("ledger replay cutoff exceeds current revision")
		}
		rev = throughRevision
		recorded = ledgerdomain.ZeroTime
		if rev > 0 {
			recorded, err = q.EventRecordedAt(ctx, rev)
			if err != nil {
				return nil, err
			}
		}
	}
	if !ledgerdomain.CanonicalUTCString(recorded) {
		return nil, errors.New("ledger recorded_at is not canonical UTC")
	}
	events, err := q.ReplayEvents(ctx, policy, rev)
	if err != nil {
		return nil, err
	}
	return ledgerdomain.Replay(policy, rev, recorded, events)
}

func (s *UseCases) CurrentSnapshot(ctx context.Context) (*ledgerdomain.PortfolioSnapshot, error) {
	var result *ledgerdomain.PortfolioSnapshot
	err := s.store.Read(ctx, func(scope Reader) error { var err error; result, err = Snapshot(ctx, scope); return err })
	if err != nil {
		return nil, err
	}
	return result, nil
}
