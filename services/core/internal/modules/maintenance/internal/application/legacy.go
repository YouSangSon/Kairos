package application

import (
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
)

func VerifyLegacyV11SourceManifest(manifest contracts.BackupManifest, facts BackupFacts) error {
	orders := facts.Trading.Orders
	broker := facts.Broker
	strategy := facts.Trading.Strategy
	paper := facts.Trading.Accounting
	fx := ObservationProof{facts.Market.FXSHA256, facts.Market.FXObservations}
	prices := ObservationProof{facts.Market.PricesSHA256, facts.Market.PriceObservations}
	listings := ListingProof{facts.Market.ListingsSHA256, facts.Market.ListingEvents, facts.Market.ActiveListings}
	receipt := manifest.VerificationReceipt
	if manifest.OrderStateSHA256 != orders.SHA256 || receipt.CandidateOrderStateSHA256 != orders.SHA256 ||
		manifest.OrderCount != orders.Orders || manifest.OrderEventCount != orders.Events ||
		manifest.ExecutionAuthoritySHA256 != orders.ExecutionAuthoritySHA256 || manifest.ExecutionAuthorityEventCount != orders.ExecutionAuthorityEvents ||
		manifest.RiskReservationSHA256 != orders.RiskReservationSHA256 || manifest.RiskReservationCount != orders.RiskReservations ||
		manifest.BrokerStateSHA256 != broker.SHA256 || receipt.CandidateBrokerStateSHA256 != broker.SHA256 ||
		manifest.BrokerSnapshotCount != broker.Snapshots || manifest.BrokerReconciliationCount != broker.Reconciliations ||
		manifest.StrategyRegistrySHA256 != strategy.SHA256 || receipt.CandidateStrategyRegistrySHA256 != strategy.SHA256 ||
		manifest.StrategyEvidenceCount != strategy.Evidence || manifest.StrategySelectionEventCount != strategy.Events ||
		manifest.PaperEvaluationEventCount != strategy.Evaluations || manifest.SelectedStrategyResultSHA256 != strategy.SelectedResultSHA256 ||
		manifest.PaperAccountingStateSHA256 != paper.SHA256 || receipt.CandidatePaperAccountingStateSHA256 != paper.SHA256 ||
		manifest.PaperAccountingSessionCount != paper.Sessions || manifest.PaperMarketBarObservationCount != paper.MarketBars ||
		manifest.PaperSignalEventCount != paper.Signals || manifest.PaperExecutionAuthorizationCount != paper.Authorizations ||
		manifest.PaperCapitalizedFillCount != paper.CapitalizedFills ||
		manifest.FXObservationStateSHA256 != fx.SHA256 || receipt.CandidateFXObservationStateSHA256 != fx.SHA256 || manifest.FXObservationCount != fx.Observations ||
		manifest.SecurityPriceObservationStateSHA256 != prices.SHA256 || receipt.CandidateSecurityPriceObservationStateSHA256 != prices.SHA256 || manifest.SecurityPriceObservationCount != prices.Observations ||
		manifest.InstrumentListingStateSHA256 != listings.SHA256 || receipt.CandidateInstrumentListingStateSHA256 != listings.SHA256 ||
		manifest.InstrumentListingEventCount != listings.Events || manifest.ActiveInstrumentListingCount != listings.Active {
		return errors.New("v11 source recovery proof mismatch")
	}
	return nil
}

func VerifyLegacyV12SourceManifest(manifest contracts.BackupManifest, facts BackupFacts) error {
	if err := VerifyLegacyV11SourceManifest(manifest, facts); err != nil {
		return fmt.Errorf("v12 source prerequisite proof: %w", err)
	}
	performance := facts.Trading.Performance
	receipt := manifest.VerificationReceipt
	if manifest.PaperPerformanceStateSHA256 != performance.SHA256 ||
		receipt.CandidatePaperPerformanceStateSHA256 != performance.SHA256 ||
		manifest.PaperPerformanceEventCount != performance.Events || manifest.PaperPerformanceMarkCount != performance.Marks {
		return errors.New("v12 source paper performance recovery proof mismatch")
	}
	return nil
}

func VerifyLegacyV13SourceManifest(manifest contracts.BackupManifest, facts BackupFacts) error {
	if err := VerifyLegacyV12SourceManifest(manifest, facts); err != nil {
		return fmt.Errorf("v13 source prerequisite proof: %w", err)
	}
	strategy := facts.Trading.StrategyPerformance
	receipt := manifest.VerificationReceipt
	if manifest.PaperStrategyPerformanceStateSHA256 != strategy.SHA256 ||
		receipt.CandidatePaperStrategyPerformanceStateSHA256 != strategy.SHA256 ||
		manifest.PaperStrategyPerformanceEventCount != strategy.Events ||
		manifest.PaperStrategyPerformanceSampleCount != strategy.Samples {
		return errors.New("v13 source paper strategy performance recovery proof mismatch")
	}
	return nil
}

func VerifyLegacyV14SourceManifest(manifest contracts.BackupManifest, facts BackupFacts) error {
	if err := VerifyLegacyV13SourceManifest(manifest, facts); err != nil {
		return fmt.Errorf("v14 source prerequisite proof: %w", err)
	}
	policy := facts.Trading.Policy
	receipt := manifest.VerificationReceipt
	if manifest.PaperPerformancePolicyStateSHA256 != policy.SHA256 ||
		receipt.CandidatePaperPerformancePolicyStateSHA256 != policy.SHA256 ||
		manifest.PaperPerformancePolicyEventCount != policy.Events ||
		manifest.PaperPerformancePolicyActionCount != policy.Actions ||
		manifest.PaperPerformancePolicyAutomaticHaltCount != policy.AutomaticHalts {
		return errors.New("v14 source paper performance policy recovery proof mismatch")
	}
	return nil
}

func PaperPromotionManifestMatches(manifest contracts.BackupManifest, proof trading.PromotionRecovery) bool {
	return manifest.PaperPromotionStateSHA256 == proof.SHA256 && manifest.PaperPromotionClaimCount == proof.Claims &&
		manifest.PaperPromotionDecisionCount == proof.Decisions && manifest.VerificationReceipt.CandidatePaperPromotionStateSHA256 == proof.SHA256
}
