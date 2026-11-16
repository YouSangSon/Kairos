package contracts

type MockSyncReceipt struct {
	Provider          string            `json:"provider"`
	Mode              BrokerEnvironment `json:"mode"`
	FetchedAt         string            `json:"fetched_at"`
	RecordedAt        string            `json:"recorded_at"`
	LedgerRevision    string            `json:"ledger_revision"`
	Positions         int               `json:"positions"`
	OpenOrders        int               `json:"open_orders"`
	AllPositionsMatch bool              `json:"all_positions_match"`
}
type ExportReceipt struct {
	SchemaVersion   string `json:"schema_version"`
	Source          string `json:"source"`
	BrokerFreshness string `json:"broker_freshness"`
	LedgerRevision  string `json:"ledger_revision"`
	RecordedAt      string `json:"recorded_at"`
	Rows            int    `json:"rows"`
	SHA256          string `json:"sha256"`
}
