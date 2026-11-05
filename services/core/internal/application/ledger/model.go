package ledger

import "omni-folio/services/core/internal/ledgerdomain"

type APIError = ledgerdomain.APIError
type Transaction = ledgerdomain.Transaction
type PreviewRow struct {
	RowNumber        int               `json:"row_number"`
	Status           string            `json:"status"`
	Transaction      *Transaction      `json:"transaction,omitempty"`
	CorrectionTarget *CorrectionTarget `json:"correction_target,omitempty"`
	DuplicateOf      string            `json:"duplicate_of,omitempty"`
	Errors           []APIError        `json:"errors,omitempty"`
	Resolution       *Resolution       `json:"resolution,omitempty"`
}

type CorrectionTarget struct {
	SourceEventID string `json:"source_event_id"`
	Type          string `json:"type"`
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
}

type Resolution struct {
	Kind           string   `json:"kind"`
	SourceField    string   `json:"source_field"`
	SourceValue    string   `json:"source_value"`
	RequiredAction string   `json:"required_action"`
	CandidateIDs   []string `json:"candidate_ids"`
}

type PreviewTotals struct {
	TotalRows      int `json:"total_rows"`
	NewRows        int `json:"new_rows"`
	DuplicateRows  int `json:"duplicate_rows"`
	ErrorRows      int `json:"error_rows"`
	UnresolvedRows int `json:"unresolved_rows"`
}

type ImportPreview struct {
	PreviewID          string        `json:"preview_id"`
	FileSHA256         string        `json:"file_sha256"`
	SchemaVersion      string        `json:"schema_version"`
	MappingVersion     string        `json:"mapping_version"`
	LedgerRevision     string        `json:"ledger_revision"`
	PreviewFingerprint string        `json:"preview_fingerprint"`
	CanApply           bool          `json:"can_apply"`
	Rows               []PreviewRow  `json:"rows"`
	Totals             PreviewTotals `json:"totals"`
}

type ApplyRequest struct {
	PreviewID      string `json:"preview_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type ApplyReceipt struct {
	ReceiptID            string `json:"receipt_id"`
	PreviewID            string `json:"preview_id"`
	IdempotencyKey       string `json:"idempotency_key"`
	FileSHA256           string `json:"file_sha256"`
	LedgerRevisionBefore string `json:"ledger_revision_before"`
	LedgerRevisionAfter  string `json:"ledger_revision_after"`
	AppliedRows          int    `json:"applied_rows"`
	SkippedDuplicateRows int    `json:"skipped_duplicate_rows"`
	ReceivedAt           string `json:"received_at"`
	RecordedAt           string `json:"recorded_at"`
}
