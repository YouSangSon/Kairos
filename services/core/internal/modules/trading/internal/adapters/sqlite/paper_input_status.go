package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"time"
)

type PaperInputStatus = contracts.PaperInputStatus

func (s *Store) PaperInputStatus(ctx context.Context, account, selection string, raw []byte) (*PaperInputStatus, error) {
	if s == nil || s.db == nil || s.now == nil || ctx == nil || !orderAlias(account, "account") || !safeOrderID(selection) {
		return nil, errors.New("paper input status identity is invalid")
	}
	proposalRaw, barsRaw, researchRaw, err := decodePaperInputBundle(raw)
	if err != nil {
		return nil, err
	}
	proposal, err := decodePaperProposal(proposalRaw)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireSchemaContext(ctx, tx); err != nil {
		return nil, err
	}
	if _, err := provePaperRunnerLeaseRecovery(ctx, tx); err != nil {
		return nil, err
	}
	registry, err := replayStrategyRegistry(ctx, tx)
	if err != nil {
		return nil, err
	}
	var selected, artifact string
	if err := tx.QueryRowContext(ctx, `SELECT selected_result_sha256 FROM strategy_selection_events WHERE event_id=?`, selection).Scan(&selected); err != nil {
		return nil, err
	}
	if selected != stringField(proposal, "strategy_result_sha256") {
		return nil, errors.New("paper input selection differs")
	}
	if err := tx.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, selected).Scan(&artifact); err != nil {
		return nil, err
	}
	evidence, err := decodeStrategyArtifact([]byte(artifact))
	if err != nil {
		return nil, err
	}
	if evidence.Target != "paper_candidate" || evidence.ParameterSHA256 != stringField(proposal, "strategy_parameter_sha256") {
		return nil, errors.New("paper input research binding differs")
	}
	session, found, err := loadPaperAccountingSession(ctx, tx, account)
	if err != nil || !found || session.ExecutionPolicySHA256 != evidence.ExecutionPolicy.SHA256 {
		return nil, errors.New("paper input session binding differs")
	}
	var research map[string]any
	if err := json.Unmarshal([]byte(artifact), &research); err != nil {
		return nil, err
	}
	if err := validatePaperResearchInput(researchRaw, stringField(research, "input_sha256"), stringField(proposal, "symbol"), stringField(proposal, "data_as_of")); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	bars, inputSHA, err := parsePaperSnapshot(barsRaw, func() time.Time { return now })
	if err != nil {
		return nil, err
	}
	last := bars[len(bars)-1]
	asOf, ok := canonicalPaperTime(stringField(proposal, "data_as_of"))
	if !ok || inputSHA != stringField(proposal, "input_sha256") || last.Symbol != stringField(proposal, "symbol") || last.CloseAt != asOf {
		return nil, errors.New("paper input CSV binding differs")
	}
	stored, _, err := loadPaperSnapshotRange(ctx, tx, bars[0], last)
	if err != nil {
		return nil, err
	}
	provided := make(map[string]bool, len(bars))
	for _, bar := range bars {
		provided[bar.CloseAt] = true
		matches := stored[bar.CloseAt]
		if len(matches) > 1 || (len(matches) == 1 && !samePaperSnapshotBar(matches[0], bar)) {
			return nil, errors.New("paper input conflicts with stored bars")
		}
	}
	for closeAt := range stored {
		if !provided[closeAt] {
			return nil, errors.New("paper input omits stored history")
		}
	}
	result := &PaperInputStatus{
		Mode: "paper_fixture_only", ObservedAt: now.Format(time.RFC3339Nano),
		ProposalSHA256: stringField(proposal, "proposal_sha256"), InputSHA256: inputSHA,
		DataAsOf: stringField(proposal, "data_as_of"), Signal: stringField(proposal, "signal"),
		CurrentSelectionMatches: registry.CurrentEventID == selection,
		BarState:                "not_observed_in_this_database", Completion: "unknown", Freshness: "unverified",
	}
	if len(stored[last.CloseAt]) == 0 {
		return result, nil
	}
	anchor := stored[last.CloseAt][0]
	if anchor.InputDataSHA256 != inputSHA {
		return nil, errors.New("paper input differs from its first receipt")
	}
	recorded, valid := parsePaperTime(anchor.RecordedAt)
	if !valid {
		return nil, errors.New("paper input first receipt time is invalid")
	}
	deadline := recorded.Add(30 * time.Second).Format(canonicalPaperTimeLayout)
	result.BarState, result.BarRecordedAt, result.ProposalDeadline = "matched", &anchor.RecordedAt, &deadline
	signalID := paperEventID("proposal", selection, result.ProposalSHA256)
	signal, found, err := loadPaperSignalEvent(ctx, tx, account, signalID)
	if err != nil {
		return nil, err
	}
	if found {
		expected := PaperSignal{
			SchemaVersion: capitalizedPaperSignalSchema, SignalID: signalID,
			SignalBarObservationID: anchor.ObservationID, StrategyResultSHA256: selected,
			StrategySelectionEventID: selection, DataSHA256: inputSHA, Symbol: last.Symbol,
			TargetQuantity: stringField(proposal, "target_quantity"), DataAsOf: asOf,
			GeneratedAt: anchor.RecordedAt, ExpiresAt: deadline,
		}
		if result.Signal == "none" || !samePaperSignalInput(*signal, expected) {
			return nil, errors.New("paper input signal binding differs")
		}
		result.SignalRecorded = true
	}
	orderID, hasOrder, err := paperOrderBySignalFrom(ctx, tx, account, signalID)
	if err != nil {
		return nil, err
	}
	if hasOrder && !found {
		return nil, errors.New("paper input order lacks its signal")
	}
	if hasOrder {
		order, err := loadOrderStateFrom(ctx, tx, orderID)
		if err != nil {
			return nil, err
		}
		result.OrderStatus = &order.Status
	}
	return result, nil
}
