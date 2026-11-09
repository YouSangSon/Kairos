package contracts

type BrokerPositionDifference struct {
	Symbol         string `json:"symbol"`
	BrokerQuantity string `json:"broker_quantity"`
	LedgerQuantity string `json:"ledger_quantity"`
	Difference     string `json:"difference"`
	Match          bool   `json:"match"`
}

type BrokerKnownGoodSnapshot struct {
	SnapshotID          string                     `json:"snapshot_id"`
	ReconciliationID    string                     `json:"reconciliation_id"`
	Provider            string                     `json:"provider"`
	Environment         BrokerEnvironment          `json:"environment"`
	Exchange            BrokerExchange             `json:"exchange"`
	AccountRef          string                     `json:"account_ref"`
	LedgerAccountID     string                     `json:"ledger_account_id"`
	FetchedAt           string                     `json:"fetched_at"`
	RecordedAt          string                     `json:"recorded_at"`
	SnapshotSHA256      string                     `json:"snapshot_sha256"`
	LedgerRevision      string                     `json:"ledger_revision"`
	AllPositionsMatch   bool                       `json:"all_positions_match"`
	PositionDifferences []BrokerPositionDifference `json:"position_differences"`
	Snapshot            BrokerSnapshot             `json:"snapshot"`
}

type BrokerReconciliationView struct {
	Provider            string                     `json:"provider"`
	Environment         BrokerEnvironment          `json:"environment"`
	Exchange            BrokerExchange             `json:"exchange"`
	Freshness           string                     `json:"freshness"`
	FetchedAt           string                     `json:"fetched_at"`
	RecordedAt          string                     `json:"recorded_at"`
	LedgerRevision      string                     `json:"ledger_revision"`
	AllPositionsMatch   bool                       `json:"all_positions_match"`
	PositionDifferences []BrokerPositionDifference `json:"position_differences"`
}

type BrokerTotals struct {
	PurchaseAmount      string `json:"purchase_amount"`
	EvaluationAmount    string `json:"evaluation_amount"`
	UnrealizedPnL       string `json:"unrealized_pnl"`
	ReturnRatePercent   string `json:"return_rate_percent"`
	EstimatedAssets     string `json:"estimated_assets"`
	LoanAmount          string `json:"loan_amount"`
	CreditLoanAmount    string `json:"credit_loan_amount"`
	CreditLendingAmount string `json:"credit_lending_amount"`
}

type BrokerPosition struct {
	Symbol               string `json:"symbol"`
	Name                 string `json:"name"`
	Quantity             string `json:"quantity"`
	TradableQuantity     string `json:"tradable_quantity"`
	AveragePurchasePrice string `json:"average_purchase_price"`
	CurrentPrice         string `json:"current_price"`
	PurchaseAmount       string `json:"purchase_amount"`
	EvaluationAmount     string `json:"evaluation_amount"`
	UnrealizedPnL        string `json:"unrealized_pnl"`
	ReturnRatePercent    string `json:"return_rate_percent"`
	WeightPercent        string `json:"weight_percent"`
}

type BrokerOpenOrder struct {
	OrderRef          string `json:"order_ref"`
	Symbol            string `json:"symbol"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	Side              string `json:"side"`
	Quantity          string `json:"quantity"`
	Price             string `json:"price"`
	RemainingQuantity string `json:"remaining_quantity"`
	Time              string `json:"time"`
	Exchange          string `json:"exchange"`
}

type BrokerSnapshot struct {
	Source        string            `json:"source"`
	Environment   BrokerEnvironment `json:"environment"`
	Exchange      BrokerExchange    `json:"exchange"`
	AccountRef    string            `json:"account_ref"`
	MaskedAccount string            `json:"masked_account"`
	FetchedAt     string            `json:"fetched_at"`
	Complete      bool              `json:"complete"`
	Currency      string            `json:"currency"`
	Totals        BrokerTotals      `json:"totals"`
	Positions     []BrokerPosition  `json:"positions"`
	OpenOrders    []BrokerOpenOrder `json:"open_orders"`
}

type BrokerEnvironment string
type BrokerExchange string
