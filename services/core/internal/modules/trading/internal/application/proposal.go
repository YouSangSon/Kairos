package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	strategydomain "omni-folio/services/core/internal/modules/trading/internal/domain/strategy"
	"time"
)

func (s *UseCases) ProcessPaperProposal(ctx context.Context, accountRef, selectionEventID string, raw []byte, barID string, fencingToken int64, admit bool, claim *paperRunnerClaim) (*PaperSignalEvent, *OrderState, error) {
	if s == nil || s.journal == nil || s.now == nil || !orderAlias(accountRef, "account") || !safeOrderID(selectionEventID) || !safeOrderID(barID) || (admit && fencingToken <= 0) {
		return nil, nil, errors.New("paper proposal admission is not configured")
	}
	proposal, err := s.inputs.Proposal(raw)
	if err != nil {
		return nil, nil, err
	}
	var event *PaperSignalEvent
	var state *OrderState
	err = s.journal.Decide(ctx, func(tx WriteScope) (bool, error) {
		var commit bool
		var err error
		event, state, commit, err = s.processPaperProposalInScope(ctx, tx, accountRef, selectionEventID, proposal, barID, fencingToken, admit, claim)
		return commit, err
	})
	if err != nil {
		return nil, nil, err
	}
	return event, state, nil
}
func (s *UseCases) processPaperProposalInScope(ctx context.Context, tx WriteScope, accountRef, selectionEventID string, proposal map[string]any, barID string, fencingToken int64, admit bool, claim *paperRunnerClaim) (*PaperSignalEvent, *OrderState, bool, error) {
	if claim != nil {
		if err := ValidatePaperRunnerLeaseTx(ctx, tx.Runner(), claim, accountRef, s.paperRunnerOwner, s.now()); err != nil {
			return nil, nil, false, err
		}
	}
	if _, err := tx.Recovery().ProvePaperAccountingRecovery(ctx); err != nil {
		return nil, nil, false, err
	}
	resultSHA := StringField(proposal, "strategy_result_sha256")
	policy, err := tx.Selections().LoadCurrentStrategyExecutionPolicy(ctx, resultSHA, selectionEventID)
	if err != nil {
		return nil, nil, false, err
	}
	artifact, err := tx.Market().EvidenceArtifact(ctx, resultSHA)
	if err != nil {
		return nil, nil, false, err
	}
	evidence, err := DecodeStrategyArtifact([]byte(artifact))
	if err != nil {
		return nil, nil, false, err
	}
	if err := domain.ValidateProposalStrategy(evidence.StrategyVersion, evidence.ParameterSHA256, evidence.Target, StringField(proposal, "strategy_parameter_sha256")); err != nil {
		return nil, nil, false, err
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewBufferString(artifact))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, nil, false, err
	}
	manifest, _ := MapField(root, "manifest")
	strategy, _ := MapField(manifest, "strategy")
	parameters, _ := MapField(strategy, "parameters")
	quantity := StringField(parameters, "quantity")
	fast, fastOK := parameters["fast_window"].(json.Number)
	slow, slowOK := parameters["slow_window"].(json.Number)
	fastN, fastErr := fast.Int64()
	slowN, slowErr := slow.Int64()
	if !ExactKeys(parameters, "quantity", "fast_window", "slow_window") || !fastOK || !slowOK || fastErr != nil || slowErr != nil {
		return nil, nil, false, errors.New("paper proposal target is not the registered strategy target")
	}
	if err := domain.ValidateProposalTarget(fastN, slowN, quantity, StringField(proposal, "signal"), StringField(proposal, "target_quantity")); err != nil {
		return nil, nil, false, err
	}
	session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, accountRef)
	if err != nil {
		return nil, nil, false, err
	}
	var sessionPolicy string
	if found {
		sessionPolicy = session.ExecutionPolicySHA256
	}
	if err := domain.ValidateProposalSession(found, sessionPolicy, policy.SHA256); err != nil {
		return nil, nil, false, err
	}
	if admit {
		if _, err := s.RequireCurrentSyntheticExecutionLease(ctx, tx, accountRef, fencingToken, s.now().UTC()); err != nil {
			return nil, nil, false, err
		}
	}
	bar, _, err := tx.Market().LoadMarketBarByID(ctx, barID)
	if err != nil {
		return nil, nil, false, err
	}
	asOf, _ := canonicalPaperTime(StringField(proposal, "data_as_of"))
	if err := domain.ValidateProposalBar(domain.PaperMarketBarObservation(*bar), StringField(proposal, "symbol"), StringField(proposal, "input_sha256"), asOf); err != nil {
		return nil, nil, false, err
	}
	recordedAt, _ := parsePaperTime(bar.RecordedAt)
	signal := PaperSignal{
		SchemaVersion: capitalizedPaperSignalSchema, SignalID: paperEventID("proposal", selectionEventID, StringField(proposal, "proposal_sha256")),
		SignalBarObservationID: barID, StrategyResultSHA256: resultSHA, StrategySelectionEventID: selectionEventID,
		DataSHA256: bar.InputDataSHA256, Symbol: bar.Symbol, TargetQuantity: StringField(proposal, "target_quantity"),
		DataAsOf: bar.CloseAt, GeneratedAt: bar.RecordedAt, ExpiresAt: recordedAt.Add(30 * time.Second).Format(canonicalPaperTimeLayout),
	}
	existing, found, err := tx.Market().LoadSignal(ctx, accountRef, signal.SignalID)
	if err != nil {
		return nil, nil, false, err
	}
	if found {
		if !samePaperSignalInput(*existing, signal) {
			return nil, nil, false, errors.New("paper proposal conflicts with its committed signal")
		}
		if _, hasOrder, err := tx.Paper().PaperOrderBySignalFrom(ctx, accountRef, signal.SignalID); err != nil {
			return nil, nil, false, err
		} else if !hasOrder {
			// A committed no-delta decision must never acquire a new order later.
			return existing, nil, false, nil
		}
		if !admit {
			return existing, nil, false, nil
		}
		event, state, err := s.AdmitPaperSignalTx(ctx, tx, accountRef, signal, fencingToken)
		return event, state, false, err
	}
	history, err := tx.Market().ProposalHistory(ctx, *bar, slowN+2)
	if err != nil {
		return nil, nil, false, err
	}
	var closes []string
	var previous string
	for _, item := range history {
		if item.CloseAt == previous || (len(closes) == 0 && item.ID != barID) {
			return nil, nil, false, errors.New("paper proposal history is ambiguous or superseded")
		}
		previous = item.CloseAt
		closes = append(closes, item.Close)
	}
	if len(closes) > int(slowN+1) {
		closes = closes[:slowN+1]
	}
	for left, right := 0, len(closes)-1; left < right; left, right = left+1, right-1 {
		closes[left], closes[right] = closes[right], closes[left]
	}
	if err := strategydomain.VerifySMACrossover(closes, fastN, slowN, StringField(proposal, "signal")); err != nil {
		return nil, nil, false, err
	}
	if StringField(proposal, "signal") == "none" {
		// No signal is not a zero target: validate timing but append no command.
		signal.TargetQuantity = "0"
		if err := validateCapitalizedPaperSignal(signal, s.now().UTC()); err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, nil
	}
	if err := validateCapitalizedPaperSignal(signal, s.now().UTC()); err != nil {
		return nil, nil, false, err
	}
	if !admit {
		return nil, nil, false, nil
	}
	event, state, err := s.AdmitPaperSignalTx(ctx, tx, accountRef, signal, fencingToken)
	if err != nil {
		return nil, nil, false, err
	}
	return event, state, true, nil
}
