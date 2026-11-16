package ledger

import (
	"context"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
	"regexp"
	"time"
)

type ReplayCapture struct {
	Policy         string
	LegacyRevision int64
}
type ReplayEvidence struct{ EventsSHA256, LegacySnapshotSHA256, SnapshotSHA256 string }
type ReplayReader interface {
	Reader
	CaptureReplayOrigin(context.Context) (ReplayCapture, error)
	VerifiedLedgerRevision(context.Context) (int64, error)
	ReplayEvidence(context.Context, int64, *PortfolioSnapshot, *PortfolioSnapshot) (ReplayEvidence, error)
}
type ReplayJournal interface {
	ReplayReader
	RecordReplayTransition(context.Context, *contracts.ReplayPreview, string) error
}

var replaySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ReplayAt(ctx context.Context, reader ReplayReader, revision int64, recordedAt string) (*contracts.ReplayPreview, error) {
	old, err := SnapshotAt(ctx, reader, ledgerdomain.LegacyFIFOCostBasisPolicy, revision)
	if err != nil {
		return nil, fmt.Errorf("legacy ledger replay: %w", err)
	}
	corrected, err := SnapshotAt(ctx, reader, ledgerdomain.FIFOCostBasisPolicy, revision)
	if err != nil {
		return nil, fmt.Errorf("corrected ledger replay: %w", err)
	}
	if !ledgerdomain.CanonicalUTCString(recordedAt) {
		return nil, errors.New("captured ledger recorded_at is not canonical UTC")
	}
	// Duplicate-only imports advance metadata without adding a new event.
	old.RecordedAt, corrected.RecordedAt = recordedAt, recordedAt
	proof, err := reader.ReplayEvidence(ctx, revision, old, corrected)
	if err != nil {
		return nil, err
	}
	return &contracts.ReplayPreview{LedgerRevision: revision, EventsSHA256: proof.EventsSHA256, LegacySnapshotSHA256: proof.LegacySnapshotSHA256, SnapshotSHA256: proof.SnapshotSHA256, LegacySnapshot: old, Snapshot: corrected}, nil
}
func prepareReplay(ctx context.Context, reader ReplayReader) (*contracts.ReplayPreview, error) {
	origin, err := reader.CaptureReplayOrigin(ctx)
	if err != nil {
		return nil, err
	}
	if err := ledgerdomain.RequireReplayTransition(origin.Policy, origin.LegacyRevision); err != nil {
		return nil, err
	}
	revision, err := reader.VerifiedLedgerRevision(ctx)
	if err != nil {
		return nil, err
	}
	if revision != origin.LegacyRevision {
		return nil, errors.New("legacy replay preparation changed the ledger revision")
	}
	metadata, err := reader.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	return ReplayAt(ctx, reader, revision, metadata.RecordedAt)
}
func (s *UseCases) PreviewReplay(ctx context.Context) (*contracts.ReplayPreview, error) {
	var result *contracts.ReplayPreview
	err := s.store.ReplayRead(ctx, func(reader ReplayReader) error { var err error; result, err = prepareReplay(ctx, reader); return err })
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *UseCases) ActivateReplay(ctx context.Context, revision int64, eventsSHA, snapshotSHA string) (*contracts.ReplayPreview, error) {
	now := s.now()
	if !replaySHA256Pattern.MatchString(eventsSHA) || !replaySHA256Pattern.MatchString(snapshotSHA) || revision <= 0 {
		return nil, errors.New("replay activation requires the exact preview revision, events SHA and corrected snapshot SHA")
	}
	var result *contracts.ReplayPreview
	err := s.store.ReplayWrite(ctx, func(journal ReplayJournal) error {
		preview, err := prepareReplay(ctx, journal)
		if err != nil {
			return err
		}
		if preview.LedgerRevision != revision || preview.EventsSHA256 != eventsSHA || preview.SnapshotSHA256 != snapshotSHA {
			return errors.New("ledger replay preview changed; review a new preview before activation")
		}
		recordedAt, err := time.Parse(time.RFC3339Nano, preview.Snapshot.RecordedAt)
		if err != nil || !ledgerdomain.ReplayActivationTimeEligible(recordedAt, now) {
			return errors.New("ledger replay activation precedes the captured ledger")
		}
		if err := journal.RecordReplayTransition(ctx, preview, now.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		result = preview
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
