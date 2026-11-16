package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	"time"
)

type ObservationProof struct {
	SHA256       string
	Observations int
}
type ListingProof struct {
	SHA256         string
	Events, Active int
}
type SeriesProof struct {
	SHA256           string
	Versions, Points int
}

func sameStrategyRegistryProof(a, b trading.StrategyRecovery) bool { return a == b }
func BuildManifest(source, candidate BackupFacts, createdAt, verifiedAt time.Time, dbSHA string, size int64, snapshotSHA string, id func(string) string) (*contracts.BackupManifest, error) {
	sourceOrders := source.Trading.Orders
	sourceBroker := source.Broker
	sourceStrategy := source.Trading.Strategy
	sourcePaperPromotion := source.Trading.Promotion
	sourcePaperAccounting := source.Trading.Accounting
	sourcePaperPerformance := source.Trading.Performance
	sourcePaperStrategyPerformance := source.Trading.StrategyPerformance
	sourcePaperPerformancePolicy := source.Trading.Policy
	sourceFX := ObservationProof{source.Market.FXSHA256, source.Market.FXObservations}
	sourceSecurityPrices := ObservationProof{source.Market.PricesSHA256, source.Market.PriceObservations}
	sourceInstrumentListings := ListingProof{source.Market.ListingsSHA256, source.Market.ListingEvents, source.Market.ActiveListings}
	sourceBenchmark := SeriesProof{source.Market.BenchmarkSHA256, source.Market.BenchmarkVersions, source.Market.BenchmarkPoints}
	sourceOwnerFX := SeriesProof{source.Market.OwnerFXSHA256, source.Market.OwnerFXVersions, source.Market.OwnerFXPoints}
	sourceOwnerPrice := SeriesProof{source.Market.OwnerPriceSHA256, source.Market.OwnerPriceVersions, source.Market.OwnerPricePoints}
	sourceWatchlist := source.Watchlist
	candidateOrders := candidate.Trading.Orders
	candidateBroker := candidate.Broker
	candidateStrategy := candidate.Trading.Strategy
	candidatePaperPromotion := candidate.Trading.Promotion
	candidatePaperAccounting := candidate.Trading.Accounting
	candidatePaperPerformance := candidate.Trading.Performance
	candidatePaperStrategyPerformance := candidate.Trading.StrategyPerformance
	candidatePaperPerformancePolicy := candidate.Trading.Policy
	candidatePaperRunnerLease := candidate.Trading.Runner
	candidateFX := ObservationProof{candidate.Market.FXSHA256, candidate.Market.FXObservations}
	candidateSecurityPrices := ObservationProof{candidate.Market.PricesSHA256, candidate.Market.PriceObservations}
	candidateInstrumentListings := ListingProof{candidate.Market.ListingsSHA256, candidate.Market.ListingEvents, candidate.Market.ActiveListings}
	candidateBenchmark := SeriesProof{candidate.Market.BenchmarkSHA256, candidate.Market.BenchmarkVersions, candidate.Market.BenchmarkPoints}
	candidateOwnerFX := SeriesProof{candidate.Market.OwnerFXSHA256, candidate.Market.OwnerFXVersions, candidate.Market.OwnerFXPoints}
	candidateOwnerPrice := SeriesProof{candidate.Market.OwnerPriceSHA256, candidate.Market.OwnerPriceVersions, candidate.Market.OwnerPricePoints}
	candidateWatchlist := candidate.Watchlist

	sourceSnapshot, err := json.Marshal(source.Ledger.Snapshot)
	if err != nil {
		return nil, err
	}
	candidateSnapshot, err := json.Marshal(candidate.Ledger.Snapshot)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(sourceSnapshot, candidateSnapshot) {
		return nil, errors.New("backup ledger recovery proof does not match source")
	}
	if sourceWatchlist != candidateWatchlist {
		return nil, errors.New("backup watchlist recovery proof does not match source")
	}
	if sourceOrders != candidateOrders {
		return nil, errors.New("backup order recovery proof does not match source")
	}
	if sourceBroker.SHA256 != candidateBroker.SHA256 || sourceBroker.Snapshots != candidateBroker.Snapshots || sourceBroker.Reconciliations != candidateBroker.Reconciliations {
		return nil, errors.New("backup broker recovery proof does not match source")
	}
	if !sameStrategyRegistryProof(sourceStrategy, candidateStrategy) {
		return nil, errors.New("backup strategy registry recovery proof does not match source")
	}
	if sourcePaperPromotion != candidatePaperPromotion {
		return nil, errors.New("backup paper promotion recovery proof does not match source")
	}
	if sourcePaperAccounting != candidatePaperAccounting {
		return nil, errors.New("backup paper accounting recovery proof does not match source")
	}
	if sourcePaperPerformance != candidatePaperPerformance {
		return nil, errors.New("backup paper performance recovery proof does not match source")
	}
	if sourcePaperStrategyPerformance != candidatePaperStrategyPerformance {
		return nil, errors.New("backup paper strategy performance recovery proof does not match source")
	}
	if sourcePaperPerformancePolicy != candidatePaperPerformancePolicy {
		return nil, errors.New("backup paper performance policy recovery proof does not match source")
	}
	if sourceFX != candidateFX {
		return nil, errors.New("backup FX observation recovery proof does not match source")
	}
	if sourceSecurityPrices != candidateSecurityPrices {
		return nil, errors.New("backup security price observation recovery proof does not match source")
	}
	if sourceInstrumentListings != candidateInstrumentListings {
		return nil, errors.New("backup instrument listing recovery proof does not match source")
	}
	if sourceBenchmark != candidateBenchmark {
		return nil, errors.New("backup benchmark series recovery proof does not match source")
	}
	if sourceOwnerFX != candidateOwnerFX {
		return nil, errors.New("backup owner FX series recovery proof does not match source")
	}
	if sourceOwnerPrice != candidateOwnerPrice {
		return nil, errors.New("backup owner price series recovery proof does not match source")
	}
	status, eligible, verificationErrors := "verified", true, []string{}
	if candidatePaperRunnerLease.Active != 0 {
		status, eligible = "rejected", false
		verificationErrors = []string{"active_paper_runner_lease_present"}
	}
	manifest := &contracts.BackupManifest{
		WatchlistStateSHA256: sourceWatchlist.SHA256, WatchlistRevision: sourceWatchlist.Revision, WatchlistEntryCount: sourceWatchlist.Entries,
		FormatVersion: backupFormat, SchemaVersion: backupSchema, CreatedAt: createdAt.Format(time.RFC3339Nano),
		SourceLedgerRevision: source.Ledger.Snapshot.LedgerRevision, OrderStateSHA256: sourceOrders.SHA256, OrderCount: sourceOrders.Orders,
		OrderEventCount: sourceOrders.Events, ExecutionAuthoritySHA256: sourceOrders.ExecutionAuthoritySHA256,
		ExecutionAuthorityEventCount: sourceOrders.ExecutionAuthorityEvents, RiskReservationSHA256: sourceOrders.RiskReservationSHA256,
		RiskReservationCount: sourceOrders.RiskReservations, BrokerStateSHA256: sourceBroker.SHA256, BrokerSnapshotCount: sourceBroker.Snapshots,
		BrokerReconciliationCount: sourceBroker.Reconciliations,
		StrategyRegistrySHA256:    sourceStrategy.SHA256, StrategyEvidenceCount: sourceStrategy.Evidence,
		StrategySelectionEventCount: sourceStrategy.Events, SelectedStrategyResultSHA256: sourceStrategy.SelectedResultSHA256,
		PaperPromotionStateSHA256: sourcePaperPromotion.SHA256, PaperPromotionClaimCount: sourcePaperPromotion.Claims, PaperPromotionDecisionCount: sourcePaperPromotion.Decisions,
		PaperEvaluationEventCount:  sourceStrategy.Evaluations,
		PaperAccountingStateSHA256: sourcePaperAccounting.SHA256, PaperAccountingSessionCount: sourcePaperAccounting.Sessions,
		PaperMarketBarObservationCount: sourcePaperAccounting.MarketBars, PaperSignalEventCount: sourcePaperAccounting.Signals,
		PaperExecutionAuthorizationCount:         sourcePaperAccounting.Authorizations,
		PaperCapitalizedFillCount:                sourcePaperAccounting.CapitalizedFills,
		PaperPerformanceStateSHA256:              sourcePaperPerformance.SHA256,
		PaperPerformanceEventCount:               sourcePaperPerformance.Events,
		PaperPerformanceMarkCount:                sourcePaperPerformance.Marks,
		PaperStrategyPerformanceStateSHA256:      sourcePaperStrategyPerformance.SHA256,
		PaperStrategyPerformanceEventCount:       sourcePaperStrategyPerformance.Events,
		PaperStrategyPerformanceSampleCount:      sourcePaperStrategyPerformance.Samples,
		PaperPerformancePolicyStateSHA256:        sourcePaperPerformancePolicy.SHA256,
		PaperPerformancePolicyEventCount:         sourcePaperPerformancePolicy.Events,
		PaperPerformancePolicyActionCount:        sourcePaperPerformancePolicy.Actions,
		PaperPerformancePolicyAutomaticHaltCount: sourcePaperPerformancePolicy.AutomaticHalts,
		PaperRunnerLeaseStateSHA256:              candidatePaperRunnerLease.SHA256,
		PaperRunnerLeaseCount:                    candidatePaperRunnerLease.Leases,
		ActivePaperRunnerLeaseCount:              candidatePaperRunnerLease.Active,
		FXObservationStateSHA256:                 sourceFX.SHA256, FXObservationCount: sourceFX.Observations,
		SecurityPriceObservationStateSHA256: sourceSecurityPrices.SHA256, SecurityPriceObservationCount: sourceSecurityPrices.Observations,
		InstrumentListingStateSHA256: sourceInstrumentListings.SHA256, InstrumentListingEventCount: sourceInstrumentListings.Events,
		ActiveInstrumentListingCount: sourceInstrumentListings.Active,
		BenchmarkSeriesStateSHA256:   sourceBenchmark.SHA256, BenchmarkSeriesVersionCount: sourceBenchmark.Versions, BenchmarkSeriesPointCount: sourceBenchmark.Points,
		OwnerFXSeriesStateSHA256: sourceOwnerFX.SHA256, OwnerFXSeriesVersionCount: sourceOwnerFX.Versions, OwnerFXSeriesPointCount: sourceOwnerFX.Points,
		OwnerPriceSeriesStateSHA256: sourceOwnerPrice.SHA256, OwnerPriceSeriesVersionCount: sourceOwnerPrice.Versions, OwnerPriceSeriesPointCount: sourceOwnerPrice.Points,
		DBSHA256: dbSHA, SizeBytes: size, ExpectedSnapshotSHA256: snapshotSHA,
		Encryption: contracts.BackupEncryption{Encrypted: false, Algorithm: "none"},
		VerificationReceipt: contracts.VerificationReceipt{
			ReceiptID: id("backup_verification"), CandidateID: id("restore_candidate"), VerifiedAt: verifiedAt.Format(time.RFC3339Nano),
			Status: status, IntegrityCheck: "ok", GoldenSnapshotCheck: "ok", OrderStateCheck: "ok", BrokerStateCheck: "ok", StrategyRegistryCheck: "ok", FXObservationCheck: "ok", SecurityPriceObservationCheck: "ok", InstrumentListingCheck: "ok", PaperPerformanceCheck: "ok", PaperStrategyPerformanceCheck: "ok", PaperPerformancePolicyCheck: "ok", PaperRunnerLeaseCheck: "ok", CandidateDBSHA256: dbSHA,
			CandidateSnapshotSHA256: snapshotSHA, CandidateOrderStateSHA256: candidateOrders.SHA256,
			CandidateBrokerStateSHA256: candidateBroker.SHA256, CandidateStrategyRegistrySHA256: candidateStrategy.SHA256,
			CandidatePaperPromotionStateSHA256:           candidatePaperPromotion.SHA256,
			CandidatePaperAccountingStateSHA256:          candidatePaperAccounting.SHA256,
			CandidatePaperPerformanceStateSHA256:         candidatePaperPerformance.SHA256,
			CandidatePaperStrategyPerformanceStateSHA256: candidatePaperStrategyPerformance.SHA256,
			CandidatePaperPerformancePolicyStateSHA256:   candidatePaperPerformancePolicy.SHA256,
			CandidatePaperRunnerLeaseStateSHA256:         candidatePaperRunnerLease.SHA256,
			CandidateFXObservationStateSHA256:            candidateFX.SHA256, CandidateSecurityPriceObservationStateSHA256: candidateSecurityPrices.SHA256,
			CandidateInstrumentListingStateSHA256: candidateInstrumentListings.SHA256, EligibleForActivation: eligible, Errors: verificationErrors,
			BenchmarkSeriesCheck: "ok", CandidateBenchmarkSeriesStateSHA256: candidateBenchmark.SHA256, CandidateBenchmarkSeriesVersionCount: candidateBenchmark.Versions, CandidateBenchmarkSeriesPointCount: candidateBenchmark.Points,
			OwnerFXSeriesCheck: "ok", CandidateOwnerFXSeriesStateSHA256: candidateOwnerFX.SHA256, CandidateOwnerFXSeriesVersionCount: candidateOwnerFX.Versions, CandidateOwnerFXSeriesPointCount: candidateOwnerFX.Points,
			OwnerPriceSeriesCheck: "ok", CandidateOwnerPriceSeriesStateSHA256: candidateOwnerPrice.SHA256, CandidateOwnerPriceSeriesVersionCount: candidateOwnerPrice.Versions, CandidateOwnerPriceSeriesPointCount: candidateOwnerPrice.Points,
			WatchlistCheck: "ok", CandidateWatchlistStateSHA256: candidateWatchlist.SHA256, CandidateWatchlistRevision: candidateWatchlist.Revision, CandidateWatchlistEntryCount: candidateWatchlist.Entries,
		},
	}

	return manifest, nil
}
