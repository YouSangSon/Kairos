package contracts

type ReplayPreview struct {
	LedgerRevision       int64              `json:"ledger_revision"`
	EventsSHA256         string             `json:"events_sha256"`
	LegacySnapshotSHA256 string             `json:"legacy_snapshot_sha256"`
	SnapshotSHA256       string             `json:"snapshot_sha256"`
	LegacySnapshot       *PortfolioSnapshot `json:"legacy_snapshot"`
	Snapshot             *PortfolioSnapshot `json:"snapshot"`
}
