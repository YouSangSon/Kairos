package domain

import (
	"context"
	"errors"
	"math/big"
)

const portfolioTWRCheckpointsPolicy = "twr_cash_flow_checkpoints.v1"
const twrCheckpointRateLimit = 2500

func twrCheckpointRate(factor *big.Rat) (string, error) {
	// An 8192-bit prefix may exceed the scalar final-rate limit before a later loss.
	rate, err := formatDecimal(quantizeHalfEven(new(big.Rat).Sub(factor, big.NewRat(1, 1)), 12))
	if err != nil || len(rate) > twrCheckpointRateLimit {
		return "", errors.New("TWR checkpoint rate exceeds its declared bound")
	}
	return rate, nil
}

func TWRCheckpoints(ctx context.Context, all Inputs) (*PortfolioTWRCheckpoints, error) {
	points := []PortfolioTWRCheckpoint{}
	report, appErr := TWR(ctx, all, &points)
	if appErr != nil {
		return nil, appErr
	}
	if report.Status != "calculated" {
		points = []PortfolioTWRCheckpoint{}
	}
	return &PortfolioTWRCheckpoints{PolicyVersion: portfolioTWRCheckpointsPolicy, Report: report, Points: points}, nil
}
