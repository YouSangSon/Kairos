package application

import (
	"context"
)

type MarketRecoveryProof struct {
	SHA256        string
	Bars, Signals int
}
type ProposalBar struct{ ID, CloseAt, Close string }
type MarketJournal interface {
	EvidenceArtifact(context.Context, string) (string, error)
	ProposalHistory(context.Context, PaperMarketBarObservation, int64) ([]ProposalBar, error)

	ProveMarketRecovery(context.Context) (MarketRecoveryProof, error)
	LoadMarketBarBySource(context.Context, string, string) (*PaperMarketBarObservation, bool, error)
	LoadMarketBarByID(context.Context, string) (*PaperMarketBarObservation, int64, error)
	InsertMarketBar(context.Context, PaperMarketBarObservation) (*PaperMarketBarObservation, error)
	LoadSignal(context.Context, string, string) (*PaperSignalEvent, bool, error)
	InsertSignal(context.Context, PaperSignalEvent) (*PaperSignalEvent, error)
	ValidatePromotionSignalCutoff(context.Context, string, string, string) error
	SignalCutoff(context.Context, PaperMarketBarObservation) (int64, int64, error)
	LegacyOrderCount(context.Context, string) (int64, error)
	SnapshotRange(context.Context, PaperMarketBarObservation, PaperMarketBarObservation) (map[string][]PaperMarketBarObservation, string, error)
}
