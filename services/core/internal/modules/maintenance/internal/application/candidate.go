package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/domain"
)

func VerifyCandidateManifest(manifest contracts.BackupManifest, policy ManifestPolicy, candidateFacts BackupFacts) error {
	receipt := manifest.VerificationReceipt
	orders := candidateFacts.Trading.Orders
	if manifest.OrderStateSHA256 != orders.SHA256 || receipt.CandidateOrderStateSHA256 != orders.SHA256 ||
		manifest.OrderCount != orders.Orders || manifest.OrderEventCount != orders.Events ||
		manifest.ExecutionAuthoritySHA256 != orders.ExecutionAuthoritySHA256 || manifest.ExecutionAuthorityEventCount != orders.ExecutionAuthorityEvents ||
		manifest.RiskReservationSHA256 != orders.RiskReservationSHA256 || manifest.RiskReservationCount != orders.RiskReservations {
		return errors.New("backup order recovery proof mismatch")
	}
	broker := candidateFacts.Broker
	if manifest.BrokerStateSHA256 != broker.SHA256 || receipt.CandidateBrokerStateSHA256 != broker.SHA256 ||
		manifest.BrokerSnapshotCount != broker.Snapshots || manifest.BrokerReconciliationCount != broker.Reconciliations {
		return errors.New("backup broker recovery proof mismatch")
	}
	strategy := candidateFacts.Trading.Strategy
	if manifest.StrategyRegistrySHA256 != strategy.SHA256 || receipt.CandidateStrategyRegistrySHA256 != strategy.SHA256 ||
		manifest.StrategyEvidenceCount != strategy.Evidence || manifest.StrategySelectionEventCount != strategy.Events ||
		manifest.SelectedStrategyResultSHA256 != strategy.SelectedResultSHA256 {
		return errors.New("backup strategy registry recovery proof mismatch")
	}
	paperPromotion := candidateFacts.Trading.Promotion
	if policy.CurrentV21Manifest {
		if !PaperPromotionManifestMatches(manifest, paperPromotion) {
			return errors.New("backup paper promotion recovery proof mismatch")
		}
	} else if !domain.LegacyPromotionEligible(paperPromotion.Claims, paperPromotion.Decisions) || paperPromotion.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains paper promotion history")
	}
	if policy.CurrentManifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.PaperEvaluationEventCount != strategy.Evaluations {
			return errors.New("backup paper evaluation recovery proof mismatch")
		}
	} else if !domain.LegacyEvaluationsEligible(strategy.Evaluations) {
		return errors.New("legacy backup unexpectedly contains paper evaluations")
	}
	paperAccounting := candidateFacts.Trading.Accounting
	if policy.CurrentManifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.PaperAccountingStateSHA256 != paperAccounting.SHA256 ||
			receipt.CandidatePaperAccountingStateSHA256 != paperAccounting.SHA256 ||
			manifest.PaperAccountingSessionCount != paperAccounting.Sessions ||
			manifest.PaperMarketBarObservationCount != paperAccounting.MarketBars ||
			manifest.PaperSignalEventCount != paperAccounting.Signals ||
			manifest.PaperExecutionAuthorizationCount != paperAccounting.Authorizations ||
			manifest.PaperCapitalizedFillCount != paperAccounting.CapitalizedFills {
			return errors.New("backup paper accounting recovery proof mismatch")
		}
	} else if policy.LegacyV10Manifest {
		if manifest.PaperAccountingSessionCount != paperAccounting.Sessions || !domain.LegacyAccountingMigrationEligible(paperAccounting.MarketBars, paperAccounting.Signals, paperAccounting.Authorizations, paperAccounting.CapitalizedFills) {
			return errors.New("v10 backup paper accounting migration proof mismatch")
		}
	} else if !domain.LegacyAccountingEligible(paperAccounting.Sessions, paperAccounting.MarketBars, paperAccounting.Signals, paperAccounting.Authorizations, paperAccounting.CapitalizedFills) {
		return errors.New("legacy backup unexpectedly contains paper accounting sessions")
	}
	paperPerformance := candidateFacts.Trading.Performance
	if policy.CurrentManifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.PaperPerformanceStateSHA256 != paperPerformance.SHA256 ||
			receipt.CandidatePaperPerformanceStateSHA256 != paperPerformance.SHA256 ||
			manifest.PaperPerformanceEventCount != paperPerformance.Events ||
			manifest.PaperPerformanceMarkCount != paperPerformance.Marks {
			return errors.New("backup paper performance recovery proof mismatch")
		}
	} else if !domain.LegacyPerformanceEligible(paperPerformance.Events, paperPerformance.Marks) ||
		paperPerformance.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains paper performance")
	}
	paperStrategyPerformance := candidateFacts.Trading.StrategyPerformance
	if policy.CurrentManifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.PaperStrategyPerformanceStateSHA256 != paperStrategyPerformance.SHA256 ||
			receipt.CandidatePaperStrategyPerformanceStateSHA256 != paperStrategyPerformance.SHA256 ||
			manifest.PaperStrategyPerformanceEventCount != paperStrategyPerformance.Events ||
			manifest.PaperStrategyPerformanceSampleCount != paperStrategyPerformance.Samples {
			return errors.New("backup paper strategy performance recovery proof mismatch")
		}
	} else if !domain.LegacyStrategyPerformanceEligible(paperStrategyPerformance.Events, paperStrategyPerformance.Samples) ||
		paperStrategyPerformance.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains paper strategy performance")
	}
	paperPerformancePolicy := candidateFacts.Trading.Policy
	if policy.CurrentManifest || policy.LegacyV14Manifest {
		if manifest.PaperPerformancePolicyStateSHA256 != paperPerformancePolicy.SHA256 ||
			receipt.CandidatePaperPerformancePolicyStateSHA256 != paperPerformancePolicy.SHA256 ||
			manifest.PaperPerformancePolicyEventCount != paperPerformancePolicy.Events ||
			manifest.PaperPerformancePolicyActionCount != paperPerformancePolicy.Actions ||
			manifest.PaperPerformancePolicyAutomaticHaltCount != paperPerformancePolicy.AutomaticHalts {
			return errors.New("backup paper performance policy recovery proof mismatch")
		}
	} else if !domain.LegacyPolicyEligible(paperPerformancePolicy.Events, paperPerformancePolicy.Actions, paperPerformancePolicy.AutomaticHalts) ||
		paperPerformancePolicy.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains paper performance policy")
	}
	paperRunnerLease := candidateFacts.Trading.Runner
	if policy.CurrentManifest {
		if manifest.PaperRunnerLeaseStateSHA256 != paperRunnerLease.SHA256 ||
			receipt.CandidatePaperRunnerLeaseStateSHA256 != paperRunnerLease.SHA256 ||
			manifest.PaperRunnerLeaseCount != paperRunnerLease.Leases ||
			manifest.ActivePaperRunnerLeaseCount != paperRunnerLease.Active || !domain.RunnerEligible(paperRunnerLease.Active) {
			return errors.New("backup paper runner lease recovery proof mismatch")
		}
	} else if !domain.LegacyRunnerMigrationEligible(paperRunnerLease.Leases, paperRunnerLease.Active) || len(paperRunnerLease.SHA256) != sha256.Size*2 {
		return errors.New("legacy backup paper runner lease migration proof mismatch")
	}
	fx := ObservationProof{candidateFacts.Market.FXSHA256, candidateFacts.Market.FXObservations}
	if policy.CurrentManifest || policy.LegacyV6Manifest || policy.LegacyV7Manifest || policy.LegacyV8Manifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.FXObservationStateSHA256 != fx.SHA256 || receipt.CandidateFXObservationStateSHA256 != fx.SHA256 ||
			manifest.FXObservationCount != fx.Observations {
			return errors.New("backup FX observation recovery proof mismatch")
		}
	} else if !domain.LegacyObservationsEligible(fx.Observations) || fx.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains FX observations")
	}
	securityPrices := ObservationProof{candidateFacts.Market.PricesSHA256, candidateFacts.Market.PriceObservations}
	if policy.CurrentManifest || policy.LegacyV7Manifest || policy.LegacyV8Manifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.SecurityPriceObservationStateSHA256 != securityPrices.SHA256 ||
			receipt.CandidateSecurityPriceObservationStateSHA256 != securityPrices.SHA256 ||
			manifest.SecurityPriceObservationCount != securityPrices.Observations {
			return errors.New("backup security price observation recovery proof mismatch")
		}
	} else if !domain.LegacyObservationsEligible(securityPrices.Observations) || securityPrices.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains security price observations")
	}
	instrumentListings := ListingProof{candidateFacts.Market.ListingsSHA256, candidateFacts.Market.ListingEvents, candidateFacts.Market.ActiveListings}
	if policy.CurrentManifest || policy.LegacyV8Manifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if manifest.InstrumentListingStateSHA256 != instrumentListings.SHA256 ||
			receipt.CandidateInstrumentListingStateSHA256 != instrumentListings.SHA256 ||
			manifest.InstrumentListingEventCount != instrumentListings.Events ||
			manifest.ActiveInstrumentListingCount != instrumentListings.Active {
			return errors.New("backup instrument listing recovery proof mismatch")
		}
	} else if !domain.LegacyListingsEligible(instrumentListings.Events, instrumentListings.Active) ||
		instrumentListings.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains instrument listings")
	}
	benchmark := SeriesProof{candidateFacts.Market.BenchmarkSHA256, candidateFacts.Market.BenchmarkVersions, candidateFacts.Market.BenchmarkPoints}
	if policy.HasBenchmarkManifest {
		if !BenchmarkManifestMatches(manifest, benchmark) {
			return errors.New("backup benchmark series proof mismatch")
		}
	} else if !domain.LegacySeriesEligible(benchmark.Versions, benchmark.Points) || benchmark.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains benchmark series")
	}
	ownerFX := SeriesProof{candidateFacts.Market.OwnerFXSHA256, candidateFacts.Market.OwnerFXVersions, candidateFacts.Market.OwnerFXPoints}
	if policy.HasOwnerFXManifest {
		if !OwnerFXManifestMatches(manifest, ownerFX) {
			return errors.New("backup owner FX series proof mismatch")
		}
	} else if !domain.LegacySeriesEligible(ownerFX.Versions, ownerFX.Points) || ownerFX.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains owner FX series")
	}
	ownerPrice := SeriesProof{candidateFacts.Market.OwnerPriceSHA256, candidateFacts.Market.OwnerPriceVersions, candidateFacts.Market.OwnerPricePoints}
	if policy.HasOwnerPriceManifest {
		if !OwnerPriceManifestMatches(manifest, ownerPrice) {
			return errors.New("backup owner price series proof mismatch")
		}
	} else if !domain.LegacySeriesEligible(ownerPrice.Versions, ownerPrice.Points) || ownerPrice.SHA256 != hex.EncodeToString(sha256.New().Sum(nil)) {
		return errors.New("legacy backup unexpectedly contains owner price series")
	}
	watchlist := candidateFacts.Watchlist
	if policy.HasWatchlistManifest {
		if !WatchlistManifestMatches(manifest, watchlist) {
			return errors.New("backup watchlist recovery proof mismatch")
		}
	} else if !domain.LegacyWatchlistEligible(watchlist.Revision, watchlist.Entries) {
		return errors.New("legacy backup unexpectedly contains a watchlist")
	}
	if manifest.SourceLedgerRevision != candidateFacts.Ledger.Snapshot.LedgerRevision {
		return errors.New("backup ledger revision mismatch")
	}
	return nil

}
