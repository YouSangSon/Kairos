package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

func (s *Store) LoadIntent(ctx context.Context, id string) (OrderIntent, error) {
	return loadOrderIntentFrom(ctx, s.db, id)
}
func (s *Store) AuthorizeOnce(ctx context.Context, id string, fence int64) (*OrderState, bool, error) {
	return s.authorizeSyntheticDispatchOnce(ctx, id, fence)
}
func (s *Store) RecordOutcome(ctx context.Context, event OrderEvent) (*OrderState, error) {
	return s.AppendOrderEvent(ctx, event)
}
func (s *Store) SubmitSyntheticLimit(ctx context.Context, id string, fence int64) (*OrderState, error) {
	return application.SubmitSyntheticLimit(ctx, s, s.id, s.dispatcher, id, fence)
}
