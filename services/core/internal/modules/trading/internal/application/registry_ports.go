package application

import "context"

type OrderRecords interface {
	FindIntentKey(context.Context, OrderIntent) (string, string, bool, error)
	RecordIntent(context.Context, OrderIntent, string, string) error
	InsertOrderEvent(context.Context, OrderEvent, string) error
}
type EvidenceRecords interface {
	FindEvidence(context.Context, string) (string, string, bool, error)
	RecordEvidence(context.Context, Evidence) error
	EvidenceTarget(context.Context, string) (string, bool, error)
	RequirePromotionAppendTime(context.Context, string) error
}
type SessionRecords interface {
	PaperOrderCount(context.Context, string) (int, error)
	InsertAccountingSession(context.Context, PaperAccountingSession) error
}
type SchemaReader interface {
	SchemaVersion(context.Context) (int, error)
}
