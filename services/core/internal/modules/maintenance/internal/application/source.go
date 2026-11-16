package application

import (
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
)

func VerifySourceManifest(manifest contracts.BackupManifest, policy ManifestPolicy, sourceFacts BackupFacts) error {
	receipt := manifest.VerificationReceipt
	if policy.LegacyV10Manifest {
		sourceOrders := sourceFacts.Trading.Orders
		sourcePaper := sourceFacts.Trading.Accounting
		if manifest.OrderStateSHA256 != sourceOrders.SHA256 || manifest.OrderCount != sourceOrders.Orders ||
			manifest.OrderEventCount != sourceOrders.Events || manifest.PaperAccountingStateSHA256 != sourcePaper.SHA256 ||
			manifest.PaperAccountingSessionCount != sourcePaper.Sessions || receipt.CandidatePaperAccountingStateSHA256 != sourcePaper.SHA256 {
			return errors.New("v10 source recovery proof mismatch")
		}
	}
	if policy.LegacyV11Manifest {
		if err := VerifyLegacyV11SourceManifest(manifest, sourceFacts); err != nil {
			return err
		}
	}
	if policy.LegacyV12Manifest {
		if err := VerifyLegacyV12SourceManifest(manifest, sourceFacts); err != nil {
			return err
		}
	}
	if policy.LegacyV13Manifest {
		if err := VerifyLegacyV13SourceManifest(manifest, sourceFacts); err != nil {
			return err
		}
	}
	if policy.LegacyV14Manifest {
		if err := VerifyLegacyV14SourceManifest(manifest, sourceFacts); err != nil {
			return err
		}
	}
	if policy.NonArchivalManifest {
		policy := sourceFacts.Trading.Policy
		if manifest.PaperPerformancePolicyStateSHA256 != policy.SHA256 || receipt.CandidatePaperPerformancePolicyStateSHA256 != policy.SHA256 ||
			manifest.PaperPerformancePolicyEventCount != policy.Events || manifest.PaperPerformancePolicyActionCount != policy.Actions ||
			manifest.PaperPerformancePolicyAutomaticHaltCount != policy.AutomaticHalts {
			return errors.New("source paper performance policy recovery proof mismatch")
		}
		runnerLease := sourceFacts.Trading.Runner
		if manifest.PaperRunnerLeaseStateSHA256 != runnerLease.SHA256 ||
			receipt.CandidatePaperRunnerLeaseStateSHA256 != runnerLease.SHA256 ||
			manifest.PaperRunnerLeaseCount != runnerLease.Leases ||
			manifest.ActivePaperRunnerLeaseCount != runnerLease.Active {
			return errors.New("source paper runner lease recovery proof mismatch")
		}
	}
	if policy.HasBenchmarkManifest {
		proof := SeriesProof{sourceFacts.Market.BenchmarkSHA256, sourceFacts.Market.BenchmarkVersions, sourceFacts.Market.BenchmarkPoints}
		if !BenchmarkManifestMatches(manifest, proof) {
			return errors.New("source benchmark series proof mismatch")
		}
	}
	if policy.HasOwnerFXManifest {
		proof := SeriesProof{sourceFacts.Market.OwnerFXSHA256, sourceFacts.Market.OwnerFXVersions, sourceFacts.Market.OwnerFXPoints}
		if !OwnerFXManifestMatches(manifest, proof) {
			return errors.New("source owner FX series proof mismatch")
		}
	}
	if policy.HasOwnerPriceManifest {
		proof := SeriesProof{sourceFacts.Market.OwnerPriceSHA256, sourceFacts.Market.OwnerPriceVersions, sourceFacts.Market.OwnerPricePoints}
		if !OwnerPriceManifestMatches(manifest, proof) {
			return errors.New("source owner price series proof mismatch")
		}
	}
	if policy.HasWatchlistManifest {
		proof := sourceFacts.Watchlist
		if !WatchlistManifestMatches(manifest, proof) {
			return errors.New("source watchlist recovery proof mismatch")
		}
	}
	if policy.CurrentV21Manifest {
		proof := sourceFacts.Trading.Promotion
		if !PaperPromotionManifestMatches(manifest, proof) {
			return errors.New("source paper promotion recovery proof mismatch")
		}
	}

	return nil
}
