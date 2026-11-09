package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
)

type ManifestPolicy struct{ CurrentV21Manifest, LegacyV20Manifest, HasWatchlistManifest, LegacyV19Manifest, HasOwnerPriceManifest, LegacyV18Manifest, HasOwnerFXManifest, LegacyV17Manifest, LegacyV16Manifest, HasBenchmarkManifest, NonArchivalManifest, LegacyV15Manifest, CurrentManifest, LegacyV14Manifest, LegacyV13Manifest, LegacyV12Manifest, LegacyV11Manifest, LegacyV5Manifest, LegacyV6Manifest, LegacyV7Manifest, LegacyV8Manifest, LegacyV9Manifest, LegacyV10Manifest, LegacyManifest bool }

func ValidateManifest(manifest contracts.BackupManifest, fields, receiptFields map[string]json.RawMessage) (ManifestPolicy, error) {
	currentV21Manifest := manifest.FormatVersion == backupFormat && manifest.SchemaVersion == backupSchema
	legacyV20Manifest := manifest.FormatVersion == "omni-folio-backup.v20" && manifest.SchemaVersion == "omni-folio.sqlite.v26"
	hasWatchlistManifest := currentV21Manifest || legacyV20Manifest
	legacyV19Manifest := manifest.FormatVersion == "omni-folio-backup.v19" && manifest.SchemaVersion == "omni-folio.sqlite.v25"
	hasOwnerPriceManifest := hasWatchlistManifest || legacyV19Manifest
	legacyV18Manifest := manifest.FormatVersion == "omni-folio-backup.v18" && manifest.SchemaVersion == "omni-folio.sqlite.v24"
	hasOwnerFXManifest := hasOwnerPriceManifest || legacyV18Manifest
	legacyV17Manifest := manifest.FormatVersion == "omni-folio-backup.v17" && manifest.SchemaVersion == "omni-folio.sqlite.v23"
	legacyV16Manifest := manifest.FormatVersion == "omni-folio-backup.v16" && manifest.SchemaVersion == "omni-folio.sqlite.v22"
	hasBenchmarkManifest := hasOwnerFXManifest || legacyV17Manifest
	// Backup format is independent of ledger replay authority: v16 and later remain nonarchival.
	nonArchivalManifest := hasBenchmarkManifest || legacyV16Manifest
	legacyV15Manifest := manifest.FormatVersion == "omni-folio-backup.v15" && manifest.SchemaVersion == "omni-folio.sqlite.v21"
	currentManifest := nonArchivalManifest || legacyV15Manifest
	legacyV14Manifest := manifest.FormatVersion == legacyPaperRunnerFormat && manifest.SchemaVersion == legacyPaperRunnerSchema
	legacyV13Manifest := manifest.FormatVersion == legacyPaperPerformancePolicyFormat && manifest.SchemaVersion == legacyPaperPerformancePolicySchema
	legacyV12Manifest := manifest.FormatVersion == legacyStrategyPerformanceFormat && manifest.SchemaVersion == legacyStrategyPerformanceSchema
	legacyV11Manifest := manifest.FormatVersion == legacyPaperPerformanceBackupFormat && manifest.SchemaVersion == legacyPaperPerformanceBackupSchema
	legacyV5Manifest := manifest.FormatVersion == legacyBackupFormat &&
		(manifest.SchemaVersion == "omni-folio.sqlite.v8" || manifest.SchemaVersion == "omni-folio.sqlite.v9")
	legacyV6Manifest := manifest.FormatVersion == legacyFXBackupFormat && manifest.SchemaVersion == "omni-folio.sqlite.v10"
	legacyV7Manifest := manifest.FormatVersion == legacyPriceBackupFormat &&
		(manifest.SchemaVersion == "omni-folio.sqlite.v11" || manifest.SchemaVersion == "omni-folio.sqlite.v12")
	legacyV8Manifest := manifest.FormatVersion == legacyListingBackupFormat && manifest.SchemaVersion == "omni-folio.sqlite.v13"
	legacyV9Manifest := manifest.FormatVersion == legacyPaperAccountingBackupFormat && manifest.SchemaVersion == legacyPaperAccountingBackupSchema
	legacyV10Manifest := manifest.FormatVersion == legacyPaperFillBackupFormat && manifest.SchemaVersion == legacyPaperFillBackupSchema
	legacyManifest := legacyV5Manifest || legacyV6Manifest || legacyV7Manifest || legacyV8Manifest || legacyV9Manifest || legacyV10Manifest || legacyV11Manifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest
	if (!currentManifest && !legacyManifest) || manifest.Encryption.Encrypted || manifest.Encryption.Algorithm != "none" {
		return ManifestPolicy{}, errors.New("unsupported backup manifest version or encryption")
	}
	if err := RequireBenchmarkManifestFields(fields, receiptFields, hasBenchmarkManifest); err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireOwnerFXManifestFields(fields, receiptFields, hasOwnerFXManifest); err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireOwnerPriceManifestFields(fields, receiptFields, hasOwnerPriceManifest); err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireWatchlistManifestFields(fields, receiptFields, hasWatchlistManifest); err != nil {
		return ManifestPolicy{}, err
	}
	for _, group := range []struct {
		fields map[string]json.RawMessage
		keys   []string
	}{
		{fields, []string{"paper_promotion_state_sha256", "paper_promotion_claim_count", "paper_promotion_decision_count"}},
		{receiptFields, []string{"candidate_paper_promotion_state_sha256"}},
	} {
		for _, key := range group.keys {
			value, present := group.fields[key]
			if currentV21Manifest && (!present || bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
				return ManifestPolicy{}, errors.New("backup omits paper promotion proof")
			}
			if !currentV21Manifest && present {
				return ManifestPolicy{}, errors.New("legacy backup cannot claim paper promotion proof")
			}
		}
	}
	if currentManifest || legacyV11Manifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest {
		if fields["paper_evaluation_event_count"] == nil || fields["paper_accounting_state_sha256"] == nil ||
			fields["paper_accounting_session_count"] == nil || fields["paper_market_bar_observation_count"] == nil ||
			fields["paper_signal_event_count"] == nil || fields["paper_execution_authorization_count"] == nil ||
			fields["paper_capitalized_fill_count"] == nil || receiptFields["candidate_paper_accounting_state_sha256"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper accounting proof")
		}
	}
	if currentManifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest {
		if fields["paper_performance_state_sha256"] == nil || fields["paper_performance_event_count"] == nil ||
			fields["paper_performance_mark_count"] == nil || receiptFields["candidate_paper_performance_state_sha256"] == nil ||
			receiptFields["paper_performance_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper performance proof")
		}
	}
	if currentManifest || legacyV13Manifest || legacyV14Manifest {
		if fields["paper_strategy_performance_state_sha256"] == nil || fields["paper_strategy_performance_event_count"] == nil ||
			fields["paper_strategy_performance_sample_count"] == nil || receiptFields["candidate_paper_strategy_performance_state_sha256"] == nil ||
			receiptFields["paper_strategy_performance_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper strategy performance proof")
		}
	}
	if currentManifest || legacyV14Manifest {
		if fields["paper_performance_policy_state_sha256"] == nil || fields["paper_performance_policy_event_count"] == nil ||
			fields["paper_performance_policy_action_count"] == nil || fields["paper_performance_policy_automatic_halt_count"] == nil ||
			receiptFields["candidate_paper_performance_policy_state_sha256"] == nil || receiptFields["paper_performance_policy_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper performance policy proof")
		}
	}
	if currentManifest {
		if fields["paper_runner_lease_state_sha256"] == nil || fields["paper_runner_lease_count"] == nil ||
			fields["active_paper_runner_lease_count"] == nil || receiptFields["candidate_paper_runner_lease_state_sha256"] == nil ||
			receiptFields["paper_runner_lease_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper runner lease proof")
		}
	}
	if legacyV14Manifest && (fields["paper_runner_lease_state_sha256"] != nil || fields["paper_runner_lease_count"] != nil ||
		fields["active_paper_runner_lease_count"] != nil || receiptFields["candidate_paper_runner_lease_state_sha256"] != nil ||
		receiptFields["paper_runner_lease_check"] != nil) {
		return ManifestPolicy{}, errors.New("v14 backup manifest contains v15 paper runner lease fields")
	}
	if legacyV13Manifest && (fields["paper_performance_policy_state_sha256"] != nil || fields["paper_performance_policy_event_count"] != nil ||
		fields["paper_performance_policy_action_count"] != nil || fields["paper_performance_policy_automatic_halt_count"] != nil ||
		receiptFields["candidate_paper_performance_policy_state_sha256"] != nil || receiptFields["paper_performance_policy_check"] != nil) {
		return ManifestPolicy{}, errors.New("v13 backup manifest contains v14 paper performance policy fields")
	}
	if legacyV12Manifest && (fields["paper_strategy_performance_state_sha256"] != nil ||
		fields["paper_strategy_performance_event_count"] != nil || fields["paper_strategy_performance_sample_count"] != nil ||
		receiptFields["candidate_paper_strategy_performance_state_sha256"] != nil ||
		receiptFields["paper_strategy_performance_check"] != nil) {
		return ManifestPolicy{}, errors.New("v12 backup manifest contains v13 paper strategy performance fields")
	}
	if legacyV11Manifest && (fields["paper_performance_state_sha256"] != nil || fields["paper_performance_event_count"] != nil ||
		fields["paper_performance_mark_count"] != nil || receiptFields["candidate_paper_performance_state_sha256"] != nil ||
		receiptFields["paper_performance_check"] != nil) {
		return ManifestPolicy{}, errors.New("v11 backup manifest contains v12 paper performance fields")
	}
	if legacyV10Manifest {
		if fields["paper_evaluation_event_count"] == nil || fields["paper_accounting_state_sha256"] == nil ||
			fields["paper_accounting_session_count"] == nil || fields["fx_observation_state_sha256"] == nil ||
			fields["fx_observation_count"] == nil || fields["security_price_observation_state_sha256"] == nil ||
			fields["security_price_observation_count"] == nil || fields["instrument_listing_state_sha256"] == nil ||
			fields["instrument_listing_event_count"] == nil || fields["active_instrument_listing_count"] == nil ||
			receiptFields["candidate_paper_accounting_state_sha256"] == nil {
			return ManifestPolicy{}, errors.New("v10 backup manifest omits paper accounting proof")
		}
		if fields["paper_market_bar_observation_count"] != nil || fields["paper_signal_event_count"] != nil ||
			fields["paper_execution_authorization_count"] != nil || fields["paper_capitalized_fill_count"] != nil {
			return ManifestPolicy{}, errors.New("v10 backup manifest contains v11 paper fields")
		}
	}
	if legacyManifest && !legacyV10Manifest && !legacyV11Manifest && !legacyV12Manifest && !legacyV13Manifest && !legacyV14Manifest {
		if fields["paper_accounting_state_sha256"] != nil || fields["paper_accounting_session_count"] != nil ||
			receiptFields["candidate_paper_accounting_state_sha256"] != nil {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains paper accounting fields")
		}
		if fields["paper_market_bar_observation_count"] != nil || fields["paper_signal_event_count"] != nil ||
			fields["paper_execution_authorization_count"] != nil || fields["paper_capitalized_fill_count"] != nil {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains v11 paper fields")
		}
		if !legacyV9Manifest && fields["paper_evaluation_event_count"] != nil {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains current paper evaluation fields")
		}
		if legacyV9Manifest && fields["paper_evaluation_event_count"] == nil {
			return ManifestPolicy{}, errors.New("v9 backup manifest omits required paper evaluation fields")
		}
		if !(legacyV8Manifest || legacyV9Manifest) && (fields["instrument_listing_state_sha256"] != nil || fields["instrument_listing_event_count"] != nil ||
			fields["active_instrument_listing_count"] != nil || receiptFields["instrument_listing_check"] != nil ||
			receiptFields["candidate_instrument_listing_state_sha256"] != nil) {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains current instrument listing fields")
		}
		if (legacyV8Manifest || legacyV9Manifest) && (fields["instrument_listing_state_sha256"] == nil || fields["instrument_listing_event_count"] == nil ||
			fields["active_instrument_listing_count"] == nil || receiptFields["instrument_listing_check"] == nil ||
			receiptFields["candidate_instrument_listing_state_sha256"] == nil) {
			return ManifestPolicy{}, errors.New("legacy backup manifest omits required instrument listing fields")
		}
		if (legacyV5Manifest || legacyV6Manifest) && (fields["security_price_observation_state_sha256"] != nil || fields["security_price_observation_count"] != nil) {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains current security price observation fields")
		}
		if (legacyV5Manifest || legacyV6Manifest) && (receiptFields["security_price_observation_check"] != nil || receiptFields["candidate_security_price_observation_state_sha256"] != nil) {
			return ManifestPolicy{}, errors.New("legacy backup receipt contains current security price observation fields")
		}
		if legacyV5Manifest && (fields["fx_observation_state_sha256"] != nil || fields["fx_observation_count"] != nil ||
			receiptFields["fx_observation_check"] != nil || receiptFields["candidate_fx_observation_state_sha256"] != nil) {
			return ManifestPolicy{}, errors.New("v5 backup manifest contains v6 FX observation fields")
		}
		if legacyV6Manifest && (fields["fx_observation_state_sha256"] == nil || fields["fx_observation_count"] == nil ||
			receiptFields["fx_observation_check"] == nil || receiptFields["candidate_fx_observation_state_sha256"] == nil) {
			return ManifestPolicy{}, errors.New("v6 backup manifest omits required FX observation fields")
		}
	}
	receipt := manifest.VerificationReceipt
	if receipt.Status != "verified" || receipt.IntegrityCheck != "ok" || receipt.GoldenSnapshotCheck != "ok" || receipt.OrderStateCheck != "ok" || receipt.BrokerStateCheck != "ok" || receipt.StrategyRegistryCheck != "ok" ||
		((currentManifest || legacyV6Manifest || legacyV7Manifest || legacyV8Manifest || legacyV9Manifest || legacyV10Manifest || legacyV11Manifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest) && receipt.FXObservationCheck != "ok") ||
		((currentManifest || legacyV7Manifest || legacyV8Manifest || legacyV9Manifest || legacyV10Manifest || legacyV11Manifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest) && receipt.SecurityPriceObservationCheck != "ok") ||
		((currentManifest || legacyV8Manifest || legacyV9Manifest || legacyV10Manifest || legacyV11Manifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest) && receipt.InstrumentListingCheck != "ok") ||
		((currentManifest || legacyV12Manifest || legacyV13Manifest || legacyV14Manifest) && receipt.PaperPerformanceCheck != "ok") ||
		((currentManifest || legacyV13Manifest || legacyV14Manifest) && receipt.PaperStrategyPerformanceCheck != "ok") ||
		((currentManifest || legacyV14Manifest) && receipt.PaperPerformancePolicyCheck != "ok") ||
		(currentManifest && receipt.PaperRunnerLeaseCheck != "ok") || !receipt.EligibleForActivation || len(receipt.Errors) != 0 {
		return ManifestPolicy{}, errors.New("backup manifest is not eligible for activation")
	}
	return ManifestPolicy{CurrentV21Manifest: currentV21Manifest, LegacyV20Manifest: legacyV20Manifest, HasWatchlistManifest: hasWatchlistManifest, LegacyV19Manifest: legacyV19Manifest, HasOwnerPriceManifest: hasOwnerPriceManifest, LegacyV18Manifest: legacyV18Manifest, HasOwnerFXManifest: hasOwnerFXManifest, LegacyV17Manifest: legacyV17Manifest, LegacyV16Manifest: legacyV16Manifest, HasBenchmarkManifest: hasBenchmarkManifest, NonArchivalManifest: nonArchivalManifest, LegacyV15Manifest: legacyV15Manifest, CurrentManifest: currentManifest, LegacyV14Manifest: legacyV14Manifest, LegacyV13Manifest: legacyV13Manifest, LegacyV12Manifest: legacyV12Manifest, LegacyV11Manifest: legacyV11Manifest, LegacyV5Manifest: legacyV5Manifest, LegacyV6Manifest: legacyV6Manifest, LegacyV7Manifest: legacyV7Manifest, LegacyV8Manifest: legacyV8Manifest, LegacyV9Manifest: legacyV9Manifest, LegacyV10Manifest: legacyV10Manifest, LegacyManifest: legacyManifest}, nil
}
