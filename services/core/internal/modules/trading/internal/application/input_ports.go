package application

import (
	"context"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
	"time"
)

type InputDecoder interface {
	Promotion(context.Context, []byte) (string, []researchdomain.Bar, error)
	Snapshot([]byte, func() time.Time) ([]PaperMarketBarObservation, string, error)
	Proposal([]byte) (map[string]any, error)
	Bundle([]byte) ([]byte, []byte, []byte, error)
	Research([]byte, string, string, string) error
}
