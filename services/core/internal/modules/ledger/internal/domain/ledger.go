package ledgerdomain

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type Transaction struct {
	EventID               string `json:"event_id"`
	SourceEventID         string `json:"source_event_id"`
	AccountID             string `json:"account_id"`
	Type                  string `json:"type"`
	OccurredAt            string `json:"occurred_at"`
	InstrumentID          string `json:"instrument_id,omitempty"`
	Symbol                string `json:"symbol,omitempty"`
	Quantity              string `json:"quantity,omitempty"`
	Price                 string `json:"price,omitempty"`
	Fee                   string `json:"fee,omitempty"`
	Currency              string `json:"currency"`
	Amount                string `json:"amount"`
	CounterCurrency       string `json:"counter_currency,omitempty"`
	CounterAmount         string `json:"counter_amount,omitempty"`
	CorrectsSourceEventID string `json:"corrects_source_event_id,omitempty"`
}

type Money struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

type Holding struct {
	InstrumentID string `json:"instrument_id"`
	Symbol       string `json:"symbol"`
	Quantity     string `json:"quantity"`
	CostBasis    string `json:"cost_basis"`
	Currency     string `json:"currency"`
}

type Provenance struct {
	EventIDs   []string `json:"event_ids"`
	ReceiptIDs []string `json:"receipt_ids"`
}

type PortfolioSnapshot struct {
	PortfolioID     string     `json:"portfolio_id"`
	LedgerRevision  string     `json:"ledger_revision"`
	CostBasisPolicy string     `json:"cost_basis_policy"`
	AsOf            string     `json:"as_of"`
	RecordedAt      string     `json:"recorded_at"`
	LiveEnabled     bool       `json:"live_enabled"`
	ValuationStatus string     `json:"valuation_status"`
	Cash            []Money    `json:"cash"`
	Holdings        []Holding  `json:"holdings"`
	RealizedPnL     []Money    `json:"realized_pnl"`
	Provenance      Provenance `json:"provenance"`
}

const FIFOCostBasisPolicy = "fifo_utc_nanosecond_exact_else_half_even_residual_8_v2"
