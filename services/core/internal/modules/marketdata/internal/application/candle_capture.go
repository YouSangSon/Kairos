package application

import (
	"context"
	"errors"
	"time"
)

// Each page commits its verified position and its bounded research observation together.
type CandleCheckpointPage struct {
	Number    int
	Digest    string
	StartedAt time.Time
	Complete  bool
	Payload   []byte
}

type CandleCheckpoint interface {
	Pages(context.Context) ([]CandleCheckpointPage, error)
	Append(context.Context, CandleCheckpointPage) error
}

var ErrCapturePrefixChanged = errors.New("saved candle page prefix changed; use a new private work directory for the changed observations")

// ResumeCandleCapture replays a fresh provider session up to the saved watermark.
// Only the new suffix is appended. A sealed observation is ready to publish offline.
func ResumeCandleCapture(ctx context.Context, journal CandleCheckpoint, now func() time.Time, fetch func(time.Time, func(CandleCheckpointPage) error) error) ([]byte, error) {
	saved, err := journal.Pages(ctx)
	if err != nil {
		return nil, err
	}
	var start time.Time
	if len(saved) > 0 {
		last := saved[len(saved)-1]
		if last.Complete {
			return last.Payload, nil
		}
		start = last.StartedAt
	} else {
		if now == nil {
			return nil, errors.New("capture clock unavailable")
		}
		start = now().UTC()
	}
	var complete []byte
	number := 0
	err = fetch(start, func(page CandleCheckpointPage) error {
		number++
		if page.Number != number || !page.StartedAt.Equal(start) || complete != nil {
			return ErrCapturePrefixChanged
		}
		if number <= len(saved) {
			if page.Digest != saved[number-1].Digest || page.Complete != saved[number-1].Complete {
				return ErrCapturePrefixChanged
			}
			return nil
		}
		if err := journal.Append(ctx, page); err != nil {
			return err
		}
		if page.Complete {
			complete = page.Payload
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if complete == nil {
		return nil, ErrCapturePrefixChanged
	}
	return complete, nil
}
