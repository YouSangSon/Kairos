package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"omni-folio/services/core/internal/paperdomain"
	"omni-folio/services/core/internal/researchdomain"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func DecodeStrategyArtifact(artifact []byte) (*Evidence, error) {
	if len(artifact) == 0 || len(artifact) > maxBodyBytes {
		return nil, errors.New("strategy evidence size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(artifact))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("strategy evidence JSON: %w", err)
	}
	if err := EnsureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if !ExactKeys(result, "schema_version", "experiment_id", "input_sha256", "config_sha256", "manifest", "execution", "evaluation", "candidates", "challenger", "promotion", "disclaimer", "result_sha256") ||
		StringField(result, "schema_version") != strategyResultSchema || StringField(result, "experiment_id") == "" ||
		!strategySHA256Pattern.MatchString(StringField(result, "input_sha256")) || !strategySHA256Pattern.MatchString(StringField(result, "config_sha256")) ||
		StringField(result, "disclaimer") != "Research-only experiment; not investment advice or a production trading authorization." {
		return nil, errors.New("strategy evidence top-level contract is invalid")
	}
	executionPolicy, err := DecodeStrategyExecutionContract(result["execution"])
	if err != nil {
		return nil, err
	}
	claimedResultSHA := StringField(result, "result_sha256")
	if !strategySHA256Pattern.MatchString(claimedResultSHA) {
		return nil, errors.New("strategy result hash is invalid")
	}
	manifest, ok := MapField(result, "manifest")
	if !ok || !ExactKeys(manifest, "strategy", "data", "engine", "evaluation_policy") {
		return nil, errors.New("strategy evidence manifest is invalid")
	}
	strategy, strategyOK := MapField(manifest, "strategy")
	data, dataOK := MapField(manifest, "data")
	engine, engineOK := MapField(manifest, "engine")
	policy, policyOK := MapField(manifest, "evaluation_policy")
	parameters, parametersOK := MapField(strategy, "parameters")
	if !strategyOK || !dataOK || !engineOK || !policyOK || !parametersOK ||
		!ExactKeys(strategy, "name", "version", "parameters", "parameter_hash") || StringField(strategy, "name") != "long_only_sma_crossover" || StringField(strategy, "version") == "" ||
		!ExactKeys(data, "version", "input_sha256") || StringField(data, "version") == "" || StringField(data, "input_sha256") != StringField(result, "input_sha256") ||
		!ExactKeys(engine, "name", "version") || StringField(engine, "name") != "omni-folio-reference" || StringField(engine, "version") != "0.1.0" ||
		!ExactKeys(policy, "version") || StringField(policy, "version") != strategyEvaluationPolicy {
		return nil, errors.New("strategy evidence manifest contract is invalid")
	}
	parameterJSON, err := StrategyCanonicalJSON(parameters)
	if err != nil {
		return nil, err
	}
	parameterHash := sha256.Sum256(parameterJSON)
	parameterSHA := hex.EncodeToString(parameterHash[:])
	if StringField(strategy, "parameter_hash") != parameterSHA {
		return nil, errors.New("strategy parameter hash mismatch")
	}
	evaluation, evaluationOK := MapField(result, "evaluation")
	candidates, candidatesOK := result["candidates"].([]any)
	challenger, challengerOK := MapField(result, "challenger")
	promotion, promotionOK := MapField(result, "promotion")
	folds, foldsOK := evaluation["folds"].([]any)
	challengerFolds, challengerFoldsOK := challenger["walk_forward_folds"].([]any)
	if !evaluationOK || !candidatesOK || !challengerOK || !promotionOK || !foldsOK || !challengerFoldsOK ||
		len(candidates) == 0 || len(folds) < 2 || len(challengerFolds) < 2 ||
		StringField(evaluation, "method") != "expanding_walk_forward_then_final_holdout" || evaluation["final_holdout_evaluated_after_selection"] != true ||
		!ExactKeys(promotion, "target", "baseline", "walk_forward_gate_passed", "final_holdout_gate_passed", "baseline_gate_passed", "failed_gates") {
		return nil, errors.New("strategy evaluation contract is invalid")
	}
	target := StringField(promotion, "target")
	gateNames := []string{"walk_forward", "final_holdout", "buy_and_hold_baseline"}
	gateFields := []string{"walk_forward_gate_passed", "final_holdout_gate_passed", "baseline_gate_passed"}
	failed, failedOK := promotion["failed_gates"].([]any)
	var expectedFailed []string
	for index, field := range gateFields {
		passed, ok := promotion[field].(bool)
		if !ok {
			return nil, errors.New("strategy promotion gates are invalid")
		}
		if !passed {
			expectedFailed = append(expectedFailed, gateNames[index])
		}
	}
	if !failedOK || len(failed) != len(expectedFailed) {
		return nil, errors.New("strategy failed gates are invalid")
	}
	for index := range failed {
		if failed[index] != expectedFailed[index] {
			return nil, errors.New("strategy failed gates are invalid")
		}
	}
	if (target == "paper_candidate") != (len(expectedFailed) == 0) || (target != "paper_candidate" && target != "no_promotion") {
		return nil, errors.New("strategy promotion target is inconsistent with its gates")
	}
	delete(result, "result_sha256")
	bodyJSON, err := StrategyCanonicalJSON(result)
	if err != nil {
		return nil, err
	}
	resultHash := sha256.Sum256(bodyJSON)
	if hex.EncodeToString(resultHash[:]) != claimedResultSHA {
		return nil, errors.New("strategy result hash mismatch")
	}
	result["result_sha256"] = claimedResultSHA
	canonicalArtifact, err := StrategyCanonicalJSON(result)
	if err != nil {
		return nil, err
	}
	artifactHash := sha256.Sum256(canonicalArtifact)
	return &Evidence{
		StrategyEvidence: contracts.StrategyEvidence{ResultSHA256: claimedResultSHA, ArtifactSHA256: hex.EncodeToString(artifactHash[:]),
			StrategyName: StringField(strategy, "name"), StrategyVersion: StringField(strategy, "version"),
			ParameterSHA256: parameterSHA, Target: target}, ArtifactJSON: string(canonicalArtifact), ExecutionPolicy: executionPolicy,
	}, nil
}

func DecodeStrategyExecutionContract(value any) (strategyExecutionPolicy, error) {
	execution, ok := value.(map[string]any)
	if !ok || !ExactKeys(execution, "starting_cash", "fee", "tax", "slippage_bps", "delay_bars", "max_participation", "signal_price", "fill_price") ||
		StringField(execution, "signal_price") != "bar_close" || StringField(execution, "fill_price") != "next_eligible_bar_open" {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	startingCash, err := parseDecimal(StringField(execution, "starting_cash"))
	if err != nil || startingCash.Sign() <= 0 {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	fee, err := parseDecimal(StringField(execution, "fee"))
	if err != nil || fee.Sign() < 0 {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	tax, err := parseDecimal(StringField(execution, "tax"))
	if err != nil || tax.Sign() < 0 || tax.Cmp(big.NewRat(1, 1)) > 0 {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	slippage, err := parseDecimal(StringField(execution, "slippage_bps"))
	if err != nil || slippage.Sign() < 0 || slippage.Cmp(big.NewRat(10000, 1)) >= 0 {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	delay, err := parseDecimal(StringField(execution, "delay_bars"))
	if err != nil || delay.Sign() <= 0 || !delay.IsInt() || !delay.Num().IsInt64() {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	participation, err := parseDecimal(StringField(execution, "max_participation"))
	if err != nil || participation.Sign() <= 0 || participation.Cmp(big.NewRat(1, 1)) > 0 {
		return strategyExecutionPolicy{}, errors.New("strategy execution contract is invalid")
	}
	canonicalJSON, err := StrategyCanonicalJSON(execution)
	if err != nil {
		return strategyExecutionPolicy{}, err
	}
	hash := sha256.Sum256(canonicalJSON)
	return strategyExecutionPolicy{
		StartingCash: StringField(execution, "starting_cash"), Fee: StringField(execution, "fee"), Tax: StringField(execution, "tax"),
		SlippageBPS: StringField(execution, "slippage_bps"), DelayBars: delay.Num().Int64(),
		MaxParticipation: StringField(execution, "max_participation"), SignalPrice: StringField(execution, "signal_price"),
		FillPrice: StringField(execution, "fill_price"), SHA256: hex.EncodeToString(hash[:]), CanonicalJSON: string(canonicalJSON),
	}, nil
}

func StrategyCanonicalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func EnsureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("strategy evidence contains trailing JSON")
	}
	return nil
}

func ExactKeys(value map[string]any, expected ...string) bool {
	if len(value) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

func MapField(value map[string]any, key string) (map[string]any, bool) {
	result, ok := value[key].(map[string]any)
	return result, ok
}

func StringField(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func ParsePaperPromotionBars(ctx context.Context, raw []byte) (string, []researchdomain.Bar, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if len(raw) == 0 || len(raw) > maxMarketDataBytes || !utf8.Valid(raw) {
		return "", nil, errors.New("paper promotion bars require UTF-8 CSV of at most 1 MiB")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = 7
	header, err := reader.Read()
	if err != nil || !slices.Equal(header, []string{"bar_at", "symbol", "open", "high", "low", "close", "volume"}) {
		return "", nil, errors.New("paper promotion bars require the canonical seven-column header")
	}
	var symbol string
	var previous time.Time
	bars := make([]researchdomain.Bar, 0)
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("paper promotion bars CSV: %w", err)
		}
		if len(bars) == maxMarketDataRows {
			return "", nil, errors.New("paper promotion bars must not exceed 500 rows")
		}
		if !kiwoomStockPattern.MatchString(row[1]) || symbol != "" && row[1] != symbol {
			return "", nil, errors.New("paper promotion bars require exactly one six-digit stock symbol")
		}
		at, err := time.Parse(time.RFC3339, row[0])
		if err != nil || at.UTC().Format(time.RFC3339) != row[0] || len(bars) > 0 && !at.After(previous) {
			return "", nil, errors.New("paper promotion bars require strictly increasing whole-second UTC timestamps")
		}
		whole, fraction, _ := strings.Cut(row[5], ".")
		if len(whole) > 12 || len(fraction) > 8 || len(row[5]) > 21 {
			return "", nil, errors.New("paper promotion close is outside the paper SMA bounds")
		}
		if err := validateMarketDataValues(row[2], row[3], row[4], row[5], row[6]); err != nil {
			return "", nil, fmt.Errorf("paper promotion bars: %w", err)
		}
		symbol, previous = row[1], at
		bars = append(bars, researchdomain.Bar{At: row[0], Open: row[2], Close: row[5], Volume: row[6]})
	}
	if len(bars) == 0 {
		return "", nil, errors.New("paper promotion bars require at least one row")
	}
	return symbol, bars, nil
}

func PaperPromotionStrategy(evidence Evidence) (researchdomain.Strategy, error) {
	invalid := errors.New("paper promotion requires a bound long_only_sma_crossover 1.0.0 with capitalized shares and bounded windows")
	if evidence.StrategyName != "long_only_sma_crossover" || evidence.StrategyVersion != "1.0.0" ||
		len(evidence.ArtifactJSON) == 0 || len(evidence.ArtifactJSON) > maxBodyBytes || !utf8.ValidString(evidence.ArtifactJSON) {
		return researchdomain.Strategy{}, invalid
	}
	decoder := json.NewDecoder(strings.NewReader(evidence.ArtifactJSON))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return researchdomain.Strategy{}, invalid
	}
	if err := EnsureJSONEOF(decoder); err != nil {
		return researchdomain.Strategy{}, invalid
	}
	manifest, _ := MapField(result, "manifest")
	strategy, _ := MapField(manifest, "strategy")
	parameters, _ := MapField(strategy, "parameters")
	fast, fastOK := ResearchReportInteger(parameters["fast_window"])
	slow, slowOK := ResearchReportInteger(parameters["slow_window"])
	quantity := StringField(parameters, "quantity")
	if !ExactKeys(strategy, "name", "version", "parameters", "parameter_hash") ||
		StringField(strategy, "name") != evidence.StrategyName || StringField(strategy, "version") != evidence.StrategyVersion ||
		!ExactKeys(parameters, "fast_window", "slow_window", "quantity") || !fastOK || !slowOK || slow <= fast || slow > 365 ||
		!paperdomain.ValidCapitalizedQuantity(quantity, false) {
		return researchdomain.Strategy{}, invalid
	}
	canonicalParameters, err := StrategyCanonicalJSON(parameters)
	if err != nil {
		return researchdomain.Strategy{}, err
	}
	hash := sha256.Sum256(canonicalParameters)
	parameterSHA := hex.EncodeToString(hash[:])
	if StringField(strategy, "parameter_hash") != parameterSHA || evidence.ParameterSHA256 != parameterSHA {
		return researchdomain.Strategy{}, invalid
	}
	return researchdomain.Strategy{Quantity: quantity, FastWindow: fast, SlowWindow: slow}, nil
}

func ComparePaperPromotion(ctx context.Context, bars []researchdomain.Bar, candidate, incumbent researchdomain.Strategy, execution researchdomain.Execution) (paperPromotionComparison, error) {
	if err := ctx.Err(); err != nil {
		return paperPromotionComparison{}, err
	}
	for _, strategy := range []researchdomain.Strategy{candidate, incumbent} {
		if strategy.FastWindow <= 0 || strategy.SlowWindow <= strategy.FastWindow || strategy.SlowWindow > 365 || !paperdomain.ValidCapitalizedQuantity(strategy.Quantity, false) {
			return paperPromotionComparison{}, errors.New("paper promotion strategy is outside the paper SMA bounds")
		}
	}
	if candidate.Quantity != incumbent.Quantity {
		return paperPromotionComparison{}, errors.New("paper promotion strategies must use identical quantities")
	}
	commonWarmup := int(max(candidate.SlowWindow, incumbent.SlowWindow))
	if len(bars) != commonWarmup+3*30 {
		return paperPromotionComparison{}, errors.New("paper promotion requires exactly the common SMA warmup and three 30-bar windows")
	}
	comparison := paperPromotionComparison{PolicyVersion: paperPromotionPolicyVersion, Windows: make([]paperPromotionWindow, 0, 3)}
	for index := range 3 {
		start := commonWarmup + index*30
		windowBars, warmup := bars[start:start+30], bars[start-commonWarmup:start]
		window := paperPromotionWindow{Start: windowBars[0].At, End: windowBars[len(windowBars)-1].At}
		var err error
		window.Candidate, _, err = researchdomain.SimulateFold(ctx, windowBars, warmup, candidate, execution, false)
		if err != nil {
			return paperPromotionComparison{}, fmt.Errorf("paper promotion window %d candidate: %w", index+1, err)
		}
		window.Incumbent, _, err = researchdomain.SimulateFold(ctx, windowBars, warmup, incumbent, execution, false)
		if err != nil {
			return paperPromotionComparison{}, fmt.Errorf("paper promotion window %d incumbent: %w", index+1, err)
		}
		window.BuyAndHold, _, err = researchdomain.SimulateFold(ctx, windowBars, warmup, candidate, execution, true)
		if err != nil {
			return paperPromotionComparison{}, fmt.Errorf("paper promotion window %d buy-and-hold: %w", index+1, err)
		}
		comparison.Windows = append(comparison.Windows, window)
	}
	var err error
	comparison.Decision, comparison.Reason, err = EvaluatePaperPromotionWindows(comparison.Windows)
	if err != nil {
		return paperPromotionComparison{}, err
	}
	return comparison, nil
}

func EvaluatePaperPromotionWindows(windows []paperPromotionWindow) (string, string, error) {
	if len(windows) != 3 {
		return "", "", errors.New("paper promotion decision requires three windows")
	}
	for _, window := range windows {
		for _, trades := range []string{window.Candidate.TradeCount, window.Incumbent.TradeCount} {
			order, err := PaperPromotionMetricOrder(trades, "1")
			if err != nil {
				return "", "", err
			}
			if order < 0 {
				return "HOLD", "insufficient_round_trips", nil
			}
		}
		for _, gate := range []struct {
			candidate, bound, reason string
			rejectedOrder            int
		}{
			{window.Candidate.AfterCostReturn, "0", "candidate_negative_return", -1},
			{window.Candidate.AfterCostReturn, window.Incumbent.AfterCostReturn, "candidate_return_below_incumbent", -1},
			{window.Candidate.MaxDrawdown, window.Incumbent.MaxDrawdown, "candidate_drawdown_above_incumbent", 1},
			{window.Candidate.Turnover, window.Incumbent.Turnover, "candidate_turnover_above_incumbent", 1},
		} {
			order, err := PaperPromotionMetricOrder(gate.candidate, gate.bound)
			if err != nil {
				return "", "", err
			}
			if order == gate.rejectedOrder {
				return "HOLD", gate.reason, nil
			}
		}
	}
	holdout := windows[2]
	for _, gate := range []struct{ bound, reason string }{
		{holdout.Incumbent.AfterCostReturn, "holdout_not_better_than_incumbent"},
		{holdout.BuyAndHold.AfterCostReturn, "holdout_not_better_than_buy_and_hold"},
	} {
		order, err := PaperPromotionMetricOrder(holdout.Candidate.AfterCostReturn, gate.bound)
		if err != nil {
			return "", "", err
		}
		if order <= 0 {
			return "HOLD", gate.reason, nil
		}
	}
	return "PROMOTE", "all_forward_gates_passed", nil
}

func PaperPromotionMetricOrder(left, right string) (int, error) {
	l, err := parseDecimal(left)
	if err != nil {
		return 0, err
	}
	r, err := parseDecimal(right)
	if err != nil {
		return 0, err
	}
	return l.Cmp(r), nil
}

func PaperResearchWholeSecond(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339, raw)
	return value, err == nil && value.Location() == time.UTC && value.Nanosecond() == 0 && value.Format(time.RFC3339) == raw
}

func ResearchReportInteger(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Int64()
	return parsed, err == nil && parsed > 0 && parsed <= 9007199254740991 && number.String() == strconv.FormatInt(parsed, 10)
}

func ResearchReportDecimal(value any, minimumSign int) (string, bool) {
	raw, ok := value.(string)
	if !ok || len(raw) > 128 {
		return "", false
	}
	parsed, err := parseDecimal(raw)
	return raw, err == nil && parsed.Sign() >= minimumSign
}

func PromotionReceipt(claim PromotionClaim, decision *PromotionDecision) *PaperPromotionReceipt {
	r := &PaperPromotionReceipt{AttemptID: claim.AttemptID, InputDataSHA256: claim.InputDataSHA256,
		ComparisonStart: claim.ComparisonStart, ComparisonEnd: claim.ComparisonEnd, Decision: "PENDING", Reason: "comparison_claimed"}
	if decision != nil {
		r.Decision, r.Reason, r.Comparison, r.SelectionEventID = decision.Decision, decision.Reason, decision.Comparison, decision.SelectionEventID
	}
	return r
}

const (
	maxBodyBytes                = 1 << 20
	maxMarketDataBytes          = 1 << 20
	maxMarketDataRows           = 500
	strategyResultSchema        = "strategy-improvement-result.v1"
	strategyEvaluationPolicy    = "sma-expanding-walk-forward.v1"
	paperPromotionPolicyVersion = "paper-forward-comparison.v1"
	paperPromotionClaimSchema   = "paper-promotion-claim.v1"
)

var (
	strategySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	kiwoomStockPattern    = regexp.MustCompile(`^[0-9]{6}$`)
)

type paperPromotionComparison = contracts.PaperPromotionComparison
type paperPromotionWindow = contracts.PaperPromotionWindow
type paperPromotionClaim = PromotionClaim
type paperPromotionDecision = PromotionDecision
type StrategyEvidence = Evidence

var validateMarketDataValues = domain.ValidateBarValues
