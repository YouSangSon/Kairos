package domain

import (
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	researchdomain "omni-folio/services/core/internal/modules/trading/internal/domain/research"
)

type StrategyExecutionTerms struct{ StartingCash, Fee, Tax, SlippageBPS, DelayBars, MaxParticipation string }

func ValidateStrategyExecutionTerms(terms StrategyExecutionTerms) (int64, error) {
	startingCash, err := exact.ParseDecimal(terms.StartingCash)
	if err != nil || startingCash.Sign() <= 0 {
		return 0, errors.New("strategy execution contract is invalid")
	}
	fee, err := exact.ParseDecimal(terms.Fee)
	if err != nil || fee.Sign() < 0 {
		return 0, errors.New("strategy execution contract is invalid")
	}
	tax, err := exact.ParseDecimal(terms.Tax)
	if err != nil || tax.Sign() < 0 || tax.Cmp(big.NewRat(1, 1)) > 0 {
		return 0, errors.New("strategy execution contract is invalid")
	}
	slippage, err := exact.ParseDecimal(terms.SlippageBPS)
	if err != nil || slippage.Sign() < 0 || slippage.Cmp(big.NewRat(10000, 1)) >= 0 {
		return 0, errors.New("strategy execution contract is invalid")
	}
	delay, err := exact.ParseDecimal(terms.DelayBars)
	if err != nil || delay.Sign() <= 0 || !delay.IsInt() || !delay.Num().IsInt64() {
		return 0, errors.New("strategy execution contract is invalid")
	}
	participation, err := exact.ParseDecimal(terms.MaxParticipation)
	if err != nil || participation.Sign() <= 0 || participation.Cmp(big.NewRat(1, 1)) > 0 {
		return 0, errors.New("strategy execution contract is invalid")
	}
	return delay.Num().Int64(), nil
}

func ValidateResearchPromotionTarget(target string, failed int) error {
	if (target == "paper_candidate") != (failed == 0) || (target != "paper_candidate" && target != "no_promotion") {
		return errors.New("strategy promotion target is inconsistent with its gates")
	}
	return nil
}

func SupportedPromotionStrategy(name, version string) bool {
	return name == "long_only_sma_crossover" && version == "1.0.0"
}

func PromotionStrategyMatches(name, version, evidenceName, evidenceVersion string) bool {
	return name == evidenceName && version == evidenceVersion
}

func ValidPromotionSMA(strategy researchdomain.Strategy) bool {
	return strategy.FastWindow > 0 && strategy.SlowWindow > strategy.FastWindow && strategy.SlowWindow <= 365 && paperdomain.ValidCapitalizedQuantity(strategy.Quantity, false)
}

func ValidateProposalStrategy(version, parameterSHA, target, proposalParameterSHA string) error {
	if version != "1.0.0" || parameterSHA != proposalParameterSHA || target != "paper_candidate" {
		return errors.New("paper proposal strategy binding is invalid")
	}
	return nil
}

func ValidateProposalTarget(fast, slow int64, quantity, signal, target string) error {
	if fast <= 0 || slow <= fast || slow > 365 ||
		!validPaperTargetQuantity(quantity) || quantity == "0" || (signal == "golden_cross" && target != quantity) {
		return errors.New("paper proposal target is not the registered strategy target")
	}
	return nil
}

func ValidateProposalSession(found bool, sessionPolicy, policySHA string) error {
	if !found || sessionPolicy != policySHA {
		return errors.New("paper proposal session does not match")
	}
	return nil
}

func ValidateProposalBar(bar PaperMarketBarObservation, symbol, inputSHA, asOf string) error {
	if bar.Symbol != symbol || bar.InputDataSHA256 != inputSHA || bar.CloseAt != asOf {
		return errors.New("paper proposal does not match its stored bar")
	}
	return nil
}
