package domain

import "errors"

type ManifestPolicy struct{ CurrentV21Manifest, LegacyV20Manifest, HasWatchlistManifest, LegacyV19Manifest, HasOwnerPriceManifest, LegacyV18Manifest, HasOwnerFXManifest, LegacyV17Manifest, LegacyV16Manifest, HasBenchmarkManifest, NonArchivalManifest, LegacyV15Manifest, CurrentManifest, LegacyV14Manifest, LegacyV13Manifest, LegacyV12Manifest, LegacyV11Manifest, LegacyV5Manifest, LegacyV6Manifest, LegacyV7Manifest, LegacyV8Manifest, LegacyV9Manifest, LegacyV10Manifest, LegacyManifest bool }

func ClassifyManifest(format, schema string, encrypted bool, algorithm string) (ManifestPolicy, error) {
	currentV21Manifest := format == backupFormat && schema == backupSchema
	legacyV20Manifest := format == "omni-folio-backup.v20" && schema == "omni-folio.sqlite.v26"
	hasWatchlistManifest := currentV21Manifest || legacyV20Manifest
	legacyV19Manifest := format == "omni-folio-backup.v19" && schema == "omni-folio.sqlite.v25"
	hasOwnerPriceManifest := hasWatchlistManifest || legacyV19Manifest
	legacyV18Manifest := format == "omni-folio-backup.v18" && schema == "omni-folio.sqlite.v24"
	hasOwnerFXManifest := hasOwnerPriceManifest || legacyV18Manifest
	legacyV17Manifest := format == "omni-folio-backup.v17" && schema == "omni-folio.sqlite.v23"
	legacyV16Manifest := format == "omni-folio-backup.v16" && schema == "omni-folio.sqlite.v22"
	hasBenchmarkManifest := hasOwnerFXManifest || legacyV17Manifest
	// Backup format is independent of ledger replay authority: v16 and later remain nonarchival.
	nonArchivalManifest := hasBenchmarkManifest || legacyV16Manifest
	legacyV15Manifest := format == "omni-folio-backup.v15" && schema == "omni-folio.sqlite.v21"
	currentManifest := nonArchivalManifest || legacyV15Manifest
	legacyV14Manifest := format == legacyPaperRunnerFormat && schema == legacyPaperRunnerSchema
	legacyV13Manifest := format == legacyPaperPerformancePolicyFormat && schema == legacyPaperPerformancePolicySchema
	legacyV12Manifest := format == legacyStrategyPerformanceFormat && schema == legacyStrategyPerformanceSchema
	legacyV11Manifest := format == legacyPaperPerformanceBackupFormat && schema == legacyPaperPerformanceBackupSchema
	legacyV5Manifest := format == legacyBackupFormat &&
		(schema == "omni-folio.sqlite.v8" || schema == "omni-folio.sqlite.v9")
	legacyV6Manifest := format == legacyFXBackupFormat && schema == "omni-folio.sqlite.v10"
	legacyV7Manifest := format == legacyPriceBackupFormat &&
		(schema == "omni-folio.sqlite.v11" || schema == "omni-folio.sqlite.v12")
	legacyV8Manifest := format == legacyListingBackupFormat && schema == "omni-folio.sqlite.v13"
	legacyV9Manifest := format == legacyPaperAccountingBackupFormat && schema == legacyPaperAccountingBackupSchema
	legacyV10Manifest := format == legacyPaperFillBackupFormat && schema == legacyPaperFillBackupSchema
	legacyManifest := legacyV5Manifest || legacyV6Manifest || legacyV7Manifest || legacyV8Manifest || legacyV9Manifest || legacyV10Manifest || legacyV11Manifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest
	if (!currentManifest && !legacyManifest) || encrypted || algorithm != "none" {
		return ManifestPolicy{}, errors.New("unsupported backup manifest version or encryption")
	}
	return ManifestPolicy{CurrentV21Manifest: currentV21Manifest, LegacyV20Manifest: legacyV20Manifest, HasWatchlistManifest: hasWatchlistManifest, LegacyV19Manifest: legacyV19Manifest, HasOwnerPriceManifest: hasOwnerPriceManifest, LegacyV18Manifest: legacyV18Manifest, HasOwnerFXManifest: hasOwnerFXManifest, LegacyV17Manifest: legacyV17Manifest, LegacyV16Manifest: legacyV16Manifest, HasBenchmarkManifest: hasBenchmarkManifest, NonArchivalManifest: nonArchivalManifest, LegacyV15Manifest: legacyV15Manifest, CurrentManifest: currentManifest, LegacyV14Manifest: legacyV14Manifest, LegacyV13Manifest: legacyV13Manifest, LegacyV12Manifest: legacyV12Manifest, LegacyV11Manifest: legacyV11Manifest, LegacyV5Manifest: legacyV5Manifest, LegacyV6Manifest: legacyV6Manifest, LegacyV7Manifest: legacyV7Manifest, LegacyV8Manifest: legacyV8Manifest, LegacyV9Manifest: legacyV9Manifest, LegacyV10Manifest: legacyV10Manifest, LegacyManifest: legacyManifest}, nil
}

type ActivationReceipt struct {
	Status, IntegrityCheck, GoldenSnapshotCheck, OrderStateCheck, BrokerStateCheck, StrategyRegistryCheck    string
	FXObservationCheck, SecurityPriceObservationCheck, InstrumentListingCheck                                string
	PaperPerformanceCheck, PaperStrategyPerformanceCheck, PaperPerformancePolicyCheck, PaperRunnerLeaseCheck string
	EligibleForActivation                                                                                    bool
	ErrorCount                                                                                               int
}

func ValidateActivation(policy ManifestPolicy, receipt ActivationReceipt) error {
	if receipt.Status != "verified" || receipt.IntegrityCheck != "ok" || receipt.GoldenSnapshotCheck != "ok" || receipt.OrderStateCheck != "ok" || receipt.BrokerStateCheck != "ok" || receipt.StrategyRegistryCheck != "ok" ||
		((policy.CurrentManifest || policy.LegacyV6Manifest || policy.LegacyV7Manifest || policy.LegacyV8Manifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest) && receipt.FXObservationCheck != "ok") ||
		((policy.CurrentManifest || policy.LegacyV7Manifest || policy.LegacyV8Manifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest) && receipt.SecurityPriceObservationCheck != "ok") ||
		((policy.CurrentManifest || policy.LegacyV8Manifest || policy.LegacyV9Manifest || policy.LegacyV10Manifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest) && receipt.InstrumentListingCheck != "ok") ||
		((policy.CurrentManifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest) && receipt.PaperPerformanceCheck != "ok") ||
		((policy.CurrentManifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest) && receipt.PaperStrategyPerformanceCheck != "ok") ||
		((policy.CurrentManifest || policy.LegacyV14Manifest) && receipt.PaperPerformancePolicyCheck != "ok") ||
		(policy.CurrentManifest && receipt.PaperRunnerLeaseCheck != "ok") || !receipt.EligibleForActivation || receipt.ErrorCount != 0 {
		return errors.New("backup manifest is not eligible for activation")
	}
	return nil
}

func RunnerEligible(active int) bool { return active == 0 }
func GoldenReplayPolicy(golden, actual string, archival bool) string {
	if archival && golden == "" {
		return actual
	}
	return golden
}
func GoldenReplayPolicyMatches(golden, actual string) bool { return golden == actual }
func LegacyPromotionEligible(claims, decisions int) bool   { return claims == 0 && decisions == 0 }
func LegacyEvaluationsEligible(evaluations int) bool       { return evaluations == 0 }
func LegacyAccountingEligible(sessions, bars, signals, authorizations, fills int) bool {
	return sessions == 0 && bars == 0 && signals == 0 && authorizations == 0 && fills == 0
}
func LegacyAccountingMigrationEligible(bars, signals, authorizations, fills int) bool {
	return bars == 0 && signals == 0 && authorizations == 0 && fills == 0
}
func LegacyPerformanceEligible(events, marks int) bool           { return events == 0 && marks == 0 }
func LegacyStrategyPerformanceEligible(events, samples int) bool { return events == 0 && samples == 0 }
func LegacyPolicyEligible(events, actions, halts int) bool {
	return events == 0 && actions == 0 && halts == 0
}
func LegacyRunnerMigrationEligible(leases, active int) bool    { return leases == 1 && active == 0 }
func LegacyObservationsEligible(observations int) bool         { return observations == 0 }
func LegacyListingsEligible(events, active int) bool           { return events == 0 && active == 0 }
func LegacySeriesEligible(versions, points int) bool           { return versions == 0 && points == 0 }
func LegacyWatchlistEligible(revision int64, entries int) bool { return revision == 0 && entries == 0 }
