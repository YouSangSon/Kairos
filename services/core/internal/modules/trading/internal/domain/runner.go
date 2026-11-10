package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const PaperRunnerLeaseScope = "paper_strategy_selection"
const PaperRunnerLeaseTTL = 30 * time.Second

type RunnerLeaseRecord struct {
	Scope                    string `json:"scope"`
	FencingToken             int64  `json:"fencing_token"`
	OwnerID                  string `json:"owner_id"`
	AccountRef               string `json:"account_ref"`
	HeartbeatAtNS            int64  `json:"heartbeat_at_ns"`
	ExpiresAtNS              int64  `json:"expires_at_ns"`
	StrategySelectionEventID string `json:"strategy_selection_event_id"`
	SelectedResultSHA256     string `json:"selected_result_sha256"`
}

func PaperRunnerTimeNS(value time.Time) (int64, error) {
	value = value.UTC()
	ns := value.UnixNano()
	if ns <= 0 || !time.Unix(0, ns).UTC().Equal(value) {
		return 0, errors.New("paper runner clock is outside Unix nanosecond range")
	}
	return ns, nil
}

func PaperRunnerExpiryNS(nowNS int64) (int64, error) {
	if nowNS > math.MaxInt64-int64(PaperRunnerLeaseTTL) {
		return 0, errors.New("paper runner lease expiry overflow")
	}
	return nowNS + int64(PaperRunnerLeaseTTL), nil
}

var ErrPaperRunnerLeaseHeld = errors.New("paper runner lease is held")

func AcquireRunnerLease(previous RunnerLeaseRecord, owner, account, selection, result string, nowNS int64) (RunnerLeaseRecord, bool, error) {
	if previous.OwnerID != "" {
		if nowNS < previous.HeartbeatAtNS {
			return RunnerLeaseRecord{}, false, errors.New("paper runner lease clock regressed")
		}
		if nowNS < previous.ExpiresAtNS {
			if previous.OwnerID == owner && previous.AccountRef == account && previous.StrategySelectionEventID == selection && previous.SelectedResultSHA256 == result {
				return previous, false, nil
			}
			return RunnerLeaseRecord{}, false, fmt.Errorf("%w by another process", ErrPaperRunnerLeaseHeld)
		}
	}
	if previous.FencingToken == math.MaxInt64 {
		return RunnerLeaseRecord{}, false, errors.New("paper runner fencing token overflow")
	}
	expires, err := PaperRunnerExpiryNS(nowNS)
	if err != nil {
		return RunnerLeaseRecord{}, false, err
	}
	return RunnerLeaseRecord{Scope: PaperRunnerLeaseScope, FencingToken: previous.FencingToken + 1, OwnerID: owner, AccountRef: account, HeartbeatAtNS: nowNS, ExpiresAtNS: expires, StrategySelectionEventID: selection, SelectedResultSHA256: result}, true, nil
}
