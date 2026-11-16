package application

import (
	"context"
	"errors"
	"time"
)

func CapturePromotionState(ctx context.Context, tx WriteScope, account string, now time.Time) (string, *PaperAccountingSession, string, StrategyRegistryProof, error) {
	var empty StrategyRegistryProof
	policyProof, err := tx.Recovery().ProvePaperPerformancePolicyRecovery(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	if err := tx.Runner().RejectLiveLease(ctx, now); err != nil {
		return "", nil, "", empty, err
	}
	selection, err := tx.Selections().ReplayStrategyRegistry(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	if err := tx.Paper().RequireIsolatedPaperAccount(ctx, account); err != nil {
		return "", nil, "", empty, err
	}
	var count int
	if value, err := tx.Paper().SessionCount(ctx); err != nil {
		return "", nil, "", empty, err
	} else {
		count = value
	}
	if count != 1 {
		return "", nil, "", empty, errors.New("paper promotion requires exactly one existing paper account session")
	}
	session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, account)
	if err != nil || !found {
		return "", nil, "", empty, errors.New("paper promotion account session is missing")
	}
	if _, err := tx.Paper().ValidatePaperAccountingSession(*session); err != nil {
		return "", nil, "", empty, err
	}
	accounts, err := tx.Paper().ReplayAccountsWithoutRegistry(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	state := accounts[account]
	for _, lots := range state.Lots {
		if len(lots) != 0 {
			return "", nil, "", empty, errors.New("paper promotion requires a flat paper account")
		}
	}
	armed, err := tx.Authority().ArmedExecutionAccounts(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	if len(armed) != 0 {
		return "", nil, "", empty, errors.New("paper promotion requires all execution authorities halted")
	}
	if err := tx.Runner().RejectIncompleteRun(ctx, account, selection.CurrentEventID); err != nil {
		return "", nil, "", empty, err
	}
	ids, err := tx.Orders().AccountOrderIDs(ctx, account)
	if err != nil {
		return "", nil, "", empty, err
	}
	for _, id := range ids {
		order, err := tx.Orders().LoadOrderStateFrom(ctx, id)
		if err != nil {
			return "", nil, "", empty, err
		}
		if order.PendingAction != "" || (order.Status != "FILLED" && order.Status != "CANCELED" && order.Status != "REJECTED" && order.Status != "RISK_REJECTED") {
			return "", nil, "", empty, errors.New("paper promotion requires every account order terminal")
		}
	}
	orderProof, err := tx.Recovery().ProveOrderRecovery(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	authoritySHA, _, err := tx.Recovery().ProveAuthorityRecovery(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	runner, err := tx.Recovery().ProvePaperRunnerLeaseRecovery(ctx)
	if err != nil {
		return "", nil, "", empty, err
	}
	var researchSequence int64
	if value, err := tx.Promotions().ResearchSequence(ctx); err != nil {
		return "", nil, "", empty, err
	} else {
		researchSequence = value
	}
	_, stateSHA, err := orderJSONHash([]any{selection.CurrentEventID, selection.SelectedResultSHA256, session, state, orderProof, authoritySHA, policyProof, runner, researchSequence})
	return stateSHA, session, state.Cash, selection, err
}
