package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
)

func (s *UseCases) LocalOrderLog(ctx context.Context) (*LocalOrderLog, error) {
	var result *LocalOrderLog
	err := s.reads.Read(ctx, func(scope ReadScope) error {
		reads := scope.Orders()
		if err := reads.ProveOrders(ctx); err != nil {
			return err
		}
		entries, err := reads.LatestOrders(ctx)
		if err != nil {
			return err
		}
		result = &LocalOrderLog{Source: "local_order_log", BrokerFreshness: "unverified", Orders: []LocalOrderView{}}
		for _, entry := range entries {
			if _, err := time.Parse(time.RFC3339Nano, entry.RecordedAt); err != nil {
				return err
			}
			intent, state, err := reads.Order(ctx, entry.OrderID)
			if err != nil {
				return err
			}
			pendingAction := state.PendingAction
			if pendingAction == "" {
				pendingAction = "none"
			}
			result.Orders = append(result.Orders, LocalOrderView{
				Mode: intent.Mode, Symbol: intent.Symbol, Side: intent.Side, OrderType: intent.OrderType,
				Quantity: state.Quantity, LimitPrice: state.LimitPrice, FilledQuantity: state.FilledQuantity,
				Currency: intent.Currency, Status: state.Status, PendingAction: pendingAction, LastRecordedAt: entry.RecordedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *UseCases) PaperMonitor(ctx context.Context) (*PaperMonitor, error) {
	var result *PaperMonitor
	completed := false
	err := s.reads.Read(ctx, func(scope ReadScope) error {
		reads := scope.Monitor()
		now := s.now().UTC()
		if err := reads.ProveRunner(ctx); err != nil {
			return err
		}
		result = &PaperMonitor{SchemaVersion: "paper-monitor.v1", Mode: "paper_fixture_only", ObservedAt: now.Format(time.RFC3339Nano), Runner: PaperMonitorRunner{State: "unowned"}}
		strategy, err := reads.Registry(ctx)
		if err != nil {
			return err
		}
		result.StrategySelected = strategy.SelectedResultSHA256 != noStrategySelection
		result.SessionCount, err = reads.SessionCount(ctx)
		if err != nil {
			return err
		}
		result.PendingPolicyCount, err = reads.PendingPolicyCount(ctx)
		if err != nil {
			return err
		}
		lease, err := reads.Lease(ctx)
		if err != nil {
			return err
		}
		if lease.OwnerID != "" {
			nowNS, err := paperRunnerTimeNS(now)
			if err != nil {
				return err
			}
			heartbeat := time.Unix(0, lease.HeartbeatAtNS).UTC().Format(time.RFC3339Nano)
			expires := time.Unix(0, lease.ExpiresAtNS).UTC().Format(time.RFC3339Nano)
			result.Runner = PaperMonitorRunner{State: "lease_recorded", HeartbeatAt: &heartbeat, ExpiresAt: &expires}
			switch {
			case nowNS < lease.HeartbeatAtNS:
				result.Runner.State = "clock_regressed"
			case nowNS >= lease.ExpiresAtNS:
				result.Runner.State = "expired"
			case lease.StrategySelectionEventID != strategy.CurrentEventID || lease.SelectedResultSHA256 != strategy.SelectedResultSHA256:
				result.Runner.State = "selection_changed"
			}
		}
		policy, window, err := reads.LatestPolicy(ctx)
		if err != nil {
			return err
		}
		if policy != nil {
			result.LatestPolicy = &PaperMonitorPolicy{AsOf: window.LatestAsOf, RecordedAt: policy.RecordedAt, Decision: policy.Decision, ReasonCode: policy.ReasonCode,
				MatchesCurrentSelection: policy.StrategySelectionEventID == strategy.CurrentEventID && policy.SelectedStrategyResultRef == strategy.SelectedResultSHA256}
		}
		completed = true
		return nil
	})
	if err != nil && !completed {
		return nil, err
	}
	return result, err
}

func (s *UseCases) PaperInputStatus(ctx context.Context, account, selection string, raw []byte) (*PaperInputStatus, error) {
	if s == nil || s.reads == nil || s.now == nil || ctx == nil || !orderAlias(account, "account") || !safeOrderID(selection) {
		return nil, errors.New("paper input status identity is invalid")
	}
	proposalRaw, barsRaw, researchRaw, err := s.inputs.Bundle(raw)
	if err != nil {
		return nil, err
	}
	proposal, err := s.inputs.Proposal(proposalRaw)
	if err != nil {
		return nil, err
	}
	var result *PaperInputStatus
	err = s.reads.InspectRead(ctx, func(scope ReadScope) error {
		reads := scope.InputStatus()
		if err := reads.RequireSchema(ctx); err != nil {
			return err
		}
		if err := reads.ProveRunner(ctx); err != nil {
			return err
		}
		registry, err := reads.Registry(ctx)
		if err != nil {
			return err
		}
		selected, err := reads.SelectionResult(ctx, selection)
		if err != nil {
			return err
		}
		if selected != StringField(proposal, "strategy_result_sha256") {
			return errors.New("paper input selection differs")
		}
		artifact, err := reads.ResearchArtifact(ctx, selected)
		if err != nil {
			return err
		}
		evidence, err := DecodeStrategyArtifact([]byte(artifact))
		if err != nil {
			return err
		}
		if evidence.Target != "paper_candidate" || evidence.ParameterSHA256 != StringField(proposal, "strategy_parameter_sha256") {
			return errors.New("paper input research binding differs")
		}
		session, found, err := reads.Session(ctx, account)
		if err != nil || !found || session.ExecutionPolicySHA256 != evidence.ExecutionPolicy.SHA256 {
			return errors.New("paper input session binding differs")
		}
		var research map[string]any
		if err := json.Unmarshal([]byte(artifact), &research); err != nil {
			return err
		}
		if err := s.inputs.Research(researchRaw, StringField(research, "input_sha256"), StringField(proposal, "symbol"), StringField(proposal, "data_as_of")); err != nil {
			return err
		}
		now := s.now().UTC()
		bars, inputSHA, err := s.inputs.Snapshot(barsRaw, func() time.Time { return now })
		if err != nil {
			return err
		}
		last := bars[len(bars)-1]
		asOf, ok := canonicalPaperTime(StringField(proposal, "data_as_of"))
		if !ok || inputSHA != StringField(proposal, "input_sha256") || last.Symbol != StringField(proposal, "symbol") || last.CloseAt != asOf {
			return errors.New("paper input CSV binding differs")
		}
		stored, err := reads.Bars(ctx, bars[0], last)
		if err != nil {
			return err
		}
		provided := make(map[string]bool, len(bars))
		for _, bar := range bars {
			provided[bar.CloseAt] = true
			matches := stored[bar.CloseAt]
			if len(matches) > 1 || (len(matches) == 1 && !domain.SamePaperSnapshotBar(domain.PaperMarketBarObservation(matches[0]), domain.PaperMarketBarObservation(bar))) {
				return errors.New("paper input conflicts with stored bars")
			}
		}
		for closeAt := range stored {
			if !provided[closeAt] {
				return errors.New("paper input omits stored history")
			}
		}
		result = &PaperInputStatus{
			Mode: "paper_fixture_only", ObservedAt: now.Format(time.RFC3339Nano),
			ProposalSHA256: StringField(proposal, "proposal_sha256"), InputSHA256: inputSHA,
			DataAsOf: StringField(proposal, "data_as_of"), Signal: StringField(proposal, "signal"),
			CurrentSelectionMatches: registry.CurrentEventID == selection,
			BarState:                "not_observed_in_this_database", Completion: "unknown", Freshness: "unverified",
		}
		if len(stored[last.CloseAt]) == 0 {
			return nil
		}
		anchor := stored[last.CloseAt][0]
		if anchor.InputDataSHA256 != inputSHA {
			return errors.New("paper input differs from its first receipt")
		}
		recorded, valid := parsePaperTime(anchor.RecordedAt)
		if !valid {
			return errors.New("paper input first receipt time is invalid")
		}
		deadline := recorded.Add(30 * time.Second).Format(canonicalPaperTimeLayout)
		result.BarState, result.BarRecordedAt, result.ProposalDeadline = "matched", &anchor.RecordedAt, &deadline
		signalID := paperEventID("proposal", selection, result.ProposalSHA256)
		signal, found, err := reads.Signal(ctx, account, signalID)
		if err != nil {
			return err
		}
		if found {
			expected := PaperSignal{
				SchemaVersion: capitalizedPaperSignalSchema, SignalID: signalID,
				SignalBarObservationID: anchor.ObservationID, StrategyResultSHA256: selected,
				StrategySelectionEventID: selection, DataSHA256: inputSHA, Symbol: last.Symbol,
				TargetQuantity: StringField(proposal, "target_quantity"), DataAsOf: asOf,
				GeneratedAt: anchor.RecordedAt, ExpiresAt: deadline,
			}
			if result.Signal == "none" || !samePaperSignalInput(*signal, expected) {
				return errors.New("paper input signal binding differs")
			}
			result.SignalRecorded = true
		}
		orderID, hasOrder, err := reads.OrderBySignal(ctx, account, signalID)
		if err != nil {
			return err
		}
		if hasOrder && !found {
			return errors.New("paper input order lacks its signal")
		}
		if hasOrder {
			order, err := reads.OrderState(ctx, orderID)
			if err != nil {
				return err
			}
			result.OrderStatus = &order.Status
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func PromotionInputs(ctx context.Context, artifact func(context.Context, string) (string, error), candidateSHA, incumbentSHA, cash, policySHA string) (researchdomain.Strategy, researchdomain.Strategy, researchdomain.Execution, error) {
	var strategies [2]researchdomain.Strategy
	var policies [2]strategyExecutionPolicy
	for i, sha := range []string{candidateSHA, incumbentSHA} {
		raw, err := artifact(ctx, sha)
		if err != nil {
			return strategies[0], strategies[1], researchdomain.Execution{}, err
		}
		evidence, err := DecodeStrategyArtifact([]byte(raw))
		if err != nil {
			return strategies[0], strategies[1], researchdomain.Execution{}, err
		}
		strategies[i], err = PaperPromotionStrategy(*evidence)
		if err != nil {
			return strategies[0], strategies[1], researchdomain.Execution{}, err
		}
		policies[i] = evidence.ExecutionPolicy
	}
	if strategies[0].Quantity != strategies[1].Quantity || policies[0].SHA256 != policies[1].SHA256 || policies[0].SHA256 != policySHA {
		return strategies[0], strategies[1], researchdomain.Execution{}, errors.New("paper promotion requires identical quantity and execution policy")
	}
	return strategies[0], strategies[1], researchdomain.Execution{StartingCash: cash, Policy: PaperExecutionPolicy(policies[0]), DelayBars: int(policies[0].DelayBars)}, nil
}

func (s *UseCases) PaperPromotionReport(ctx context.Context) (*PaperPromotionReportView, error) {
	var view *PaperPromotionReportView
	var claim *PromotionClaim
	var decision *PromotionDecision
	var candidate, incumbent researchdomain.Strategy
	var currentEventID string
	err := s.reads.Read(ctx, func(scope ReadScope) error {
		reads := scope.PromotionReport()
		view = &PaperPromotionReportView{SchemaVersion: "paper-promotion-report.v1", Mode: "paper_comparison_only", ObservedAt: s.now().UTC().Format(time.RFC3339Nano)}
		if err := reads.RequireSchema(ctx); err != nil {
			return err
		}
		registry, err := reads.Registry(ctx)
		if err != nil {
			return err
		}
		currentEventID = registry.CurrentEventID
		attemptID, exists, err := reads.LatestAttemptID(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		var found bool
		claim, decision, found, err = reads.Claim(ctx, attemptID)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("paper promotion report claim disappeared")
		}
		candidate, incumbent, _, err = PromotionInputs(ctx, reads.CandidateArtifact, claim.CandidateResultSHA256, claim.IncumbentResultSHA256, claim.StartingCash, claim.ExecutionPolicySHA256)
		return err
	})
	if err != nil {
		return nil, err
	}
	if claim != nil {
		view.Attempt, err = projectPaperPromotionReport(*claim, decision, candidate, incumbent, currentEventID)
		if err != nil {
			return nil, err
		}
	}
	return view, nil
}

func (s *UseCases) ResearchReport(ctx context.Context) (*contracts.ResearchReportView, error) {
	var view *contracts.ResearchReportView
	var artifact, recordedAt string
	err := s.reads.Read(ctx, func(scope ReadScope) error {
		reads := scope.ResearchReport()
		view = &contracts.ResearchReportView{SchemaVersion: "research-report.v1", Mode: "research_artifact_only", MetricsVerification: "reported_not_recomputed", ObservedAt: s.now().UTC().Format(time.RFC3339Nano)}
		if _, err := reads.Registry(ctx); err != nil {
			return err
		}
		var err error
		artifact, recordedAt, err = reads.LatestArtifact(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	if artifact != "" {
		view.Report, err = projectResearchReport(artifact, recordedAt)
		if err != nil {
			return nil, err
		}
	}
	return view, nil
}

func projectPaperPromotionReport(claim PromotionClaim, decision *PromotionDecision, candidate, incumbent researchdomain.Strategy, currentEventID string) (*StoredPaperPromotionReport, error) {
	claimedAt, ok := parsePaperTime(claim.RecordedAt)
	if !ok {
		return nil, errors.New("invalid paper comparison claim time")
	}
	report := &StoredPaperPromotionReport{
		Symbol: claim.Symbol, ClaimedAt: claimedAt.UTC().Format(time.RFC3339Nano),
		ComparisonStart: claim.ComparisonStart, ComparisonEnd: claim.ComparisonEnd,
		Candidate: PaperPromotionReportStrategy{FastWindow: candidate.FastWindow, SlowWindow: candidate.SlowWindow, Quantity: candidate.Quantity},
		Incumbent: PaperPromotionReportStrategy{FastWindow: incumbent.FastWindow, SlowWindow: incumbent.SlowWindow, Quantity: incumbent.Quantity},
		Decision:  "PENDING", ReasonCode: "comparison_claimed", PromotionSelectionStatus: "not_promoted",
	}
	if decision == nil {
		return report, nil
	}
	decidedAt, ok := parsePaperTime(decision.RecordedAt)
	if !ok {
		return nil, errors.New("invalid paper comparison decision time")
	}
	publicTime := decidedAt.UTC().Format(time.RFC3339Nano)
	report.DecidedAt = &publicTime
	report.Decision, report.ReasonCode = decision.Decision, decision.Reason
	if decision.Decision == "PROMOTE" {
		report.PromotionSelectionStatus = "superseded"
		if decision.SelectionEventID == currentEventID {
			report.PromotionSelectionStatus = "current"
		}
	}
	if decision.Comparison == nil {
		return report, nil
	}
	comparison := decision.Comparison
	report.Comparison = &PaperPromotionReportComparison{
		PolicyVersion: comparison.PolicyVersion, MetricsVerification: "recomputed_from_stored_input",
		Decision: comparison.Decision, ReasonCode: comparison.Reason,
		Windows: make([]PaperPromotionReportWindow, 0, len(comparison.Windows)),
	}
	for _, window := range comparison.Windows {
		candidate, err := ProjectPaperPromotionReportMetrics(window.Candidate)
		if err != nil {
			return nil, err
		}
		incumbent, err := ProjectPaperPromotionReportMetrics(window.Incumbent)
		if err != nil {
			return nil, err
		}
		baseline, err := ProjectPaperPromotionReportMetrics(window.BuyAndHold)
		if err != nil {
			return nil, err
		}
		report.Comparison.Windows = append(report.Comparison.Windows, PaperPromotionReportWindow{Start: window.Start, End: window.End, Candidate: candidate, Incumbent: incumbent, BuyAndHold: baseline})
	}
	return report, nil
}

func ProjectPaperPromotionReportMetrics(value contracts.PromotionMetrics) (PaperPromotionReportMetrics, error) {
	returns, returnOK := ResearchReportDecimal(value.AfterCostReturn, -1)
	drawdown, drawdownOK := ResearchReportDecimal(value.MaxDrawdown, 0)
	trips, tripsOK := ResearchReportDecimal(value.TradeCount, 0)
	turnover, turnoverOK := ResearchReportDecimal(value.Turnover, 0)
	if !returnOK || !drawdownOK || !tripsOK || !turnoverOK || strings.Contains(trips, ".") || value.TradeCountDefinition != "completed_long_only_round_trips" {
		return PaperPromotionReportMetrics{}, errors.New("unsupported public paper comparison metrics")
	}
	return PaperPromotionReportMetrics{AfterCostReturn: returns, MaxDrawdown: drawdown, RoundTrips: trips, Turnover: turnover}, nil
}

func projectResearchReport(artifact, recordedAt string) (*contracts.StoredResearchReport, error) {
	invalid := errors.New("unsupported research report")
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewBufferString(artifact))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, invalid
	}
	manifest, _ := MapField(result, "manifest")
	strategy, _ := MapField(manifest, "strategy")
	parameters, _ := MapField(strategy, "parameters")
	challenger, _ := MapField(result, "challenger")
	selected, _ := MapField(challenger, "parameters")
	evaluation, _ := MapField(result, "evaluation")
	promotion, _ := MapField(result, "promotion")
	baseline, _ := MapField(promotion, "baseline")
	fast, fastOK := ResearchReportInteger(parameters["fast_window"])
	slow, slowOK := ResearchReportInteger(parameters["slow_window"])
	quantity, quantityOK := ResearchReportDecimal(parameters["quantity"], 1)
	// Reader support is intentionally narrower than historical registry admission.
	if StringField(strategy, "version") != "1.0.0" || !ExactKeys(parameters, "fast_window", "slow_window", "quantity") ||
		!ExactKeys(selected, "fast_window", "slow_window") || !fastOK || !slowOK || fast >= slow || !quantityOK ||
		selected["fast_window"] != parameters["fast_window"] || selected["slow_window"] != parameters["slow_window"] ||
		StringField(baseline, "name") != "buy_and_hold" {
		return nil, invalid
	}
	folds, _ := evaluation["folds"].([]any)
	candidates, _ := result["candidates"].([]any)
	foldJSON, err := StrategyCanonicalJSON(folds)
	if err != nil {
		return nil, invalid
	}
	challengerFoldJSON, err := StrategyCanonicalJSON(challenger["walk_forward_folds"])
	if err != nil || !bytes.Equal(foldJSON, challengerFoldJSON) {
		return nil, invalid
	}
	holdoutRange, _ := MapField(evaluation, "final_holdout")
	first, last := StringField(holdoutRange, "first_bar_at"), StringField(holdoutRange, "last_bar_at")
	barCount, countOK := ResearchReportInteger(holdoutRange["bar_count"])
	if !canonicalUTCString(first) || !canonicalUTCString(last) || !countOK {
		return nil, invalid
	}
	firstTime, _ := time.Parse(time.RFC3339Nano, first)
	lastTime, _ := time.Parse(time.RFC3339Nano, last)
	if firstTime.After(lastTime) || (barCount == 1) != firstTime.Equal(lastTime) {
		return nil, invalid
	}
	walkForward, walkOK := researchReportMetrics(challenger["walk_forward_metrics"], "sum_of_completed_long_only_round_trips_across_walk_forward_folds")
	holdout, holdOK := researchReportMetrics(challenger["final_holdout_metrics"], "completed_long_only_round_trips")
	buyAndHold, baselineOK := researchReportMetrics(baseline["final_holdout_metrics"], "completed_long_only_round_trips")
	if !walkOK || !holdOK || !baselineOK {
		return nil, invalid
	}
	failed := make([]string, 0)
	// Registry replay has already checked these fixed gate names and their order.
	for _, gate := range promotion["failed_gates"].([]any) {
		failed = append(failed, gate.(string))
	}
	return &contracts.StoredResearchReport{RecordedAt: recordedAt, FastWindow: fast, SlowWindow: slow, Quantity: quantity,
		CandidateCount: len(candidates), FoldCount: len(folds), FinalHoldout: contracts.ResearchReportRange{first, last, barCount},
		PromotionTarget: StringField(promotion, "target"), FailedGates: failed, WalkForward: walkForward, Holdout: holdout, BuyAndHold: buyAndHold}, nil
}

func researchReportMetrics(value any, countDefinition string) (contracts.ResearchReportMetrics, bool) {
	metrics, ok := value.(map[string]any)
	if !ok || StringField(metrics, "trade_count_definition") != countDefinition {
		return contracts.ResearchReportMetrics{}, false
	}
	returns, returnOK := ResearchReportDecimal(metrics["after_cost_return"], -1)
	drawdown, drawdownOK := ResearchReportDecimal(metrics["max_drawdown"], 0)
	trips, tripsOK := ResearchReportDecimal(metrics["trade_count"], 0)
	if !returnOK || !drawdownOK || !tripsOK || bytes.ContainsRune([]byte(trips), '.') {
		return contracts.ResearchReportMetrics{}, false
	}
	// Fixed fees can make cash negative: reported losses and drawdown may exceed 100%.
	return contracts.ResearchReportMetrics{returns, drawdown, trips}, true
}
