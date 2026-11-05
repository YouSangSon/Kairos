package ledger

import (
	"context"
	"omni-folio/services/core/internal/ledgerdomain"
)

type Metadata struct {
	Revision       int64
	RecordedAt     string
	LastVerifiedAt *string
}
type ReplayOrigin struct {
	Policy   string
	Revision int64
}
type EventRange struct{ First, Last, Count int64 }
type StoredPreview struct {
	PreviewID, FileSHA256      string
	LedgerRevision             int64
	CanApply                   bool
	JSON, ExpiresAt, CreatedAt string
}
type StoredReceipt struct{ IdempotencyKey, RequestSHA256, ReceiptID, JSON string }

type Reader interface {
	CurrentRevision(context.Context) (int64, error)
	Metadata(context.Context) (Metadata, error)
	ReplayOrigin(context.Context) (ReplayOrigin, error)
	EventRange(context.Context) (EventRange, error)
	EventRecordedAt(context.Context, int64) (string, error)
	ReplayEvents(context.Context, string, int64) ([]ledgerdomain.ReplayEvent, error)
	TransactionBySource(context.Context, string, string) (*ledgerdomain.Transaction, error)
	CashVoidCount(context.Context, string, string) (int, error)
}

type WriteScope interface {
	Reader
	Receipt(context.Context, string) (StoredReceipt, error)
	Preview(context.Context, string) (StoredPreview, error)
	RecordPreview(context.Context, StoredPreview) error
	RecordEvent(context.Context, ledgerdomain.Transaction, string, string) error
	RecordAppliedRevision(context.Context, int64, string) error
	RecordReceipt(context.Context, StoredReceipt) error
}

type Repository interface {
	Write(context.Context, func(WriteScope) error) error
	Read(context.Context, func(Reader) error) error
}
