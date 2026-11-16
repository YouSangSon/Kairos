package contracts

type LedgerActivityPage struct {
	Source          string           `json:"source"`
	BrokerFreshness string           `json:"broker_freshness"`
	LedgerRevision  string           `json:"ledger_revision"`
	RecordedAt      string           `json:"recorded_at"`
	Events          []LedgerActivity `json:"events"`
	NextCursor      *string          `json:"next_cursor"`
}

type LedgerActivity struct {
	Type            string  `json:"type"`
	OccurredAt      string  `json:"occurred_at"`
	RecordedAt      string  `json:"recorded_at"`
	Symbol          *string `json:"symbol"`
	Quantity        *string `json:"quantity"`
	Price           *string `json:"price"`
	Fee             *string `json:"fee"`
	Currency        string  `json:"currency"`
	Amount          string  `json:"amount"`
	CounterCurrency *string `json:"counter_currency"`
	CounterAmount   *string `json:"counter_amount"`
	IsCorrection    bool    `json:"is_correction"`
}

type ActivityCursor struct {
	Version          int    `json:"v"`
	LedgerRevision   int64  `json:"ledger_revision"`
	LedgerRecordedAt string `json:"ledger_recorded_at"`
	OccurredAt       string `json:"occurred_at"`
	Sequence         int64  `json:"sequence"`
}

type InstrumentLedger struct {
	InstrumentID      string           `json:"instrument_id"`
	Symbol            string           `json:"symbol"`
	Currency          string           `json:"currency"`
	Source            string           `json:"source"`
	BrokerFreshness   string           `json:"broker_freshness"`
	LedgerRevision    string           `json:"ledger_revision"`
	LedgerAsOf        string           `json:"ledger_as_of"`
	LedgerRecordedAt  string           `json:"ledger_recorded_at"`
	CostBasisPolicy   string           `json:"cost_basis_policy"`
	AverageCostPolicy string           `json:"average_cost_policy"`
	Quantity          string           `json:"quantity"`
	CostBasis         string           `json:"cost_basis"`
	AverageCost       *string          `json:"average_cost"`
	TradeLimit        int              `json:"trade_limit"`
	HasMore           bool             `json:"has_more"`
	Trades            []LedgerActivity `json:"trades"`
}
