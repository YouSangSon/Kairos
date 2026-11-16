package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
	"regexp"
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
	if err := domain.ValidateResearchPromotionTarget(target, len(expectedFailed)); err != nil {
		return nil, err
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
	delay, err := domain.ValidateStrategyExecutionTerms(domain.StrategyExecutionTerms{StartingCash: StringField(execution, "starting_cash"), Fee: StringField(execution, "fee"), Tax: StringField(execution, "tax"), SlippageBPS: StringField(execution, "slippage_bps"), DelayBars: StringField(execution, "delay_bars"), MaxParticipation: StringField(execution, "max_participation")})
	if err != nil {
		return strategyExecutionPolicy{}, err
	}
	canonicalJSON, err := StrategyCanonicalJSON(execution)
	if err != nil {
		return strategyExecutionPolicy{}, err
	}
	hash := sha256.Sum256(canonicalJSON)
	return strategyExecutionPolicy{
		StartingCash: StringField(execution, "starting_cash"), Fee: StringField(execution, "fee"), Tax: StringField(execution, "tax"),
		SlippageBPS: StringField(execution, "slippage_bps"), DelayBars: delay,
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

func PaperPromotionStrategy(evidence Evidence) (researchdomain.Strategy, error) {
	invalid := errors.New("paper promotion requires a bound long_only_sma_crossover 1.0.0 with capitalized shares and bounded windows")
	if !domain.SupportedPromotionStrategy(evidence.StrategyName, evidence.StrategyVersion) ||
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
		!domain.PromotionStrategyMatches(StringField(strategy, "name"), StringField(strategy, "version"), evidence.StrategyName, evidence.StrategyVersion) ||
		!ExactKeys(parameters, "fast_window", "slow_window", "quantity") || !fastOK || !slowOK || !domain.ValidPromotionSMA(researchdomain.Strategy{FastWindow: fast, SlowWindow: slow, Quantity: quantity}) {
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
	comparison, err := domain.ComparePaperPromotion(ctx, bars, candidate, incumbent, execution)
	return PromotionComparisonFromDomain(comparison), err
}

func EvaluatePaperPromotionWindows(windows []paperPromotionWindow) (string, string, error) {
	var values []domain.PaperPromotionWindow
	if windows != nil {
		values = make([]domain.PaperPromotionWindow, len(windows))
	}
	for i, w := range windows {
		values[i] = PromotionWindowToDomain(w)
	}
	return domain.EvaluatePaperPromotionWindows(values)
}

func PaperPromotionMetricOrder(left, right string) (int, error) {
	return domain.PaperPromotionMetricOrder(left, right)
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
