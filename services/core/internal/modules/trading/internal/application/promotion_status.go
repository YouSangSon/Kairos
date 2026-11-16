package application

import (
	"context"
	"errors"
)

func (s *UseCases) PaperPromotionStatus(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error) {
	if !safeOrderID(attemptID) {
		return nil, errors.New("paper promotion attempt ID is invalid")
	}
	claim, decision, found, err := s.promotionReader.PromotionStatusContext(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("paper promotion attempt does not exist")
	}
	return PromotionReceipt(*claim, decision), nil
}
func (s *UseCases) AbortPaperPromotion(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error) {
	if !safeOrderID(attemptID) {
		return nil, errors.New("paper promotion attempt ID is invalid")
	}
	claim, _, found, err := s.promotionReader.LoadPromotion(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("paper promotion attempt does not exist")
	}
	return s.FinishPaperPromotion(ctx, *claim, paperPromotionComparison{}, nil, true)
}
