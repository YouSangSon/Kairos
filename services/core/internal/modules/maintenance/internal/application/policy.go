package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	"omni-folio/services/core/internal/modules/maintenance/internal/domain"
)

type ManifestPolicy = domain.ManifestPolicy

func ValidateManifest(manifest contracts.BackupManifest, fields, receiptFields map[string]json.RawMessage) (ManifestPolicy, error) {
	policy, err := domain.ClassifyManifest(manifest.FormatVersion, manifest.SchemaVersion, manifest.Encryption.Encrypted, manifest.Encryption.Algorithm)
	if err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireBenchmarkManifestFields(fields, receiptFields, policy.HasBenchmarkManifest); err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireOwnerFXManifestFields(fields, receiptFields, policy.HasOwnerFXManifest); err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireOwnerPriceManifestFields(fields, receiptFields, policy.HasOwnerPriceManifest); err != nil {
		return ManifestPolicy{}, err
	}
	if err := RequireWatchlistManifestFields(fields, receiptFields, policy.HasWatchlistManifest); err != nil {
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
			if policy.CurrentV21Manifest && (!present || bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
				return ManifestPolicy{}, errors.New("backup omits paper promotion proof")
			}
			if !policy.CurrentV21Manifest && present {
				return ManifestPolicy{}, errors.New("legacy backup cannot claim paper promotion proof")
			}
		}
	}
	if policy.CurrentManifest || policy.LegacyV11Manifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if fields["paper_evaluation_event_count"] == nil || fields["paper_accounting_state_sha256"] == nil ||
			fields["paper_accounting_session_count"] == nil || fields["paper_market_bar_observation_count"] == nil ||
			fields["paper_signal_event_count"] == nil || fields["paper_execution_authorization_count"] == nil ||
			fields["paper_capitalized_fill_count"] == nil || receiptFields["candidate_paper_accounting_state_sha256"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper accounting proof")
		}
	}
	if policy.CurrentManifest || policy.LegacyV12Manifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if fields["paper_performance_state_sha256"] == nil || fields["paper_performance_event_count"] == nil ||
			fields["paper_performance_mark_count"] == nil || receiptFields["candidate_paper_performance_state_sha256"] == nil ||
			receiptFields["paper_performance_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper performance proof")
		}
	}
	if policy.CurrentManifest || policy.LegacyV13Manifest || policy.LegacyV14Manifest {
		if fields["paper_strategy_performance_state_sha256"] == nil || fields["paper_strategy_performance_event_count"] == nil ||
			fields["paper_strategy_performance_sample_count"] == nil || receiptFields["candidate_paper_strategy_performance_state_sha256"] == nil ||
			receiptFields["paper_strategy_performance_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper strategy performance proof")
		}
	}
	if policy.CurrentManifest || policy.LegacyV14Manifest {
		if fields["paper_performance_policy_state_sha256"] == nil || fields["paper_performance_policy_event_count"] == nil ||
			fields["paper_performance_policy_action_count"] == nil || fields["paper_performance_policy_automatic_halt_count"] == nil ||
			receiptFields["candidate_paper_performance_policy_state_sha256"] == nil || receiptFields["paper_performance_policy_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper performance policy proof")
		}
	}
	if policy.CurrentManifest {
		if fields["paper_runner_lease_state_sha256"] == nil || fields["paper_runner_lease_count"] == nil ||
			fields["active_paper_runner_lease_count"] == nil || receiptFields["candidate_paper_runner_lease_state_sha256"] == nil ||
			receiptFields["paper_runner_lease_check"] == nil {
			return ManifestPolicy{}, errors.New("current backup manifest omits paper runner lease proof")
		}
	}
	if policy.LegacyV14Manifest && (fields["paper_runner_lease_state_sha256"] != nil || fields["paper_runner_lease_count"] != nil ||
		fields["active_paper_runner_lease_count"] != nil || receiptFields["candidate_paper_runner_lease_state_sha256"] != nil ||
		receiptFields["paper_runner_lease_check"] != nil) {
		return ManifestPolicy{}, errors.New("v14 backup manifest contains v15 paper runner lease fields")
	}
	if policy.LegacyV13Manifest && (fields["paper_performance_policy_state_sha256"] != nil || fields["paper_performance_policy_event_count"] != nil ||
		fields["paper_performance_policy_action_count"] != nil || fields["paper_performance_policy_automatic_halt_count"] != nil ||
		receiptFields["candidate_paper_performance_policy_state_sha256"] != nil || receiptFields["paper_performance_policy_check"] != nil) {
		return ManifestPolicy{}, errors.New("v13 backup manifest contains v14 paper performance policy fields")
	}
	if policy.LegacyV12Manifest && (fields["paper_strategy_performance_state_sha256"] != nil ||
		fields["paper_strategy_performance_event_count"] != nil || fields["paper_strategy_performance_sample_count"] != nil ||
		receiptFields["candidate_paper_strategy_performance_state_sha256"] != nil ||
		receiptFields["paper_strategy_performance_check"] != nil) {
		return ManifestPolicy{}, errors.New("v12 backup manifest contains v13 paper strategy performance fields")
	}
	if policy.LegacyV11Manifest && (fields["paper_performance_state_sha256"] != nil || fields["paper_performance_event_count"] != nil ||
		fields["paper_performance_mark_count"] != nil || receiptFields["candidate_paper_performance_state_sha256"] != nil ||
		receiptFields["paper_performance_check"] != nil) {
		return ManifestPolicy{}, errors.New("v11 backup manifest contains v12 paper performance fields")
	}
	if policy.LegacyV10Manifest {
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
	if policy.LegacyManifest && !policy.LegacyV10Manifest && !policy.LegacyV11Manifest && !policy.LegacyV12Manifest && !policy.LegacyV13Manifest && !policy.LegacyV14Manifest {
		if fields["paper_accounting_state_sha256"] != nil || fields["paper_accounting_session_count"] != nil ||
			receiptFields["candidate_paper_accounting_state_sha256"] != nil {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains paper accounting fields")
		}
		if fields["paper_market_bar_observation_count"] != nil || fields["paper_signal_event_count"] != nil ||
			fields["paper_execution_authorization_count"] != nil || fields["paper_capitalized_fill_count"] != nil {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains v11 paper fields")
		}
		if !policy.LegacyV9Manifest && fields["paper_evaluation_event_count"] != nil {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains current paper evaluation fields")
		}
		if policy.LegacyV9Manifest && fields["paper_evaluation_event_count"] == nil {
			return ManifestPolicy{}, errors.New("v9 backup manifest omits required paper evaluation fields")
		}
		if !(policy.LegacyV8Manifest || policy.LegacyV9Manifest) && (fields["instrument_listing_state_sha256"] != nil || fields["instrument_listing_event_count"] != nil ||
			fields["active_instrument_listing_count"] != nil || receiptFields["instrument_listing_check"] != nil ||
			receiptFields["candidate_instrument_listing_state_sha256"] != nil) {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains current instrument listing fields")
		}
		if (policy.LegacyV8Manifest || policy.LegacyV9Manifest) && (fields["instrument_listing_state_sha256"] == nil || fields["instrument_listing_event_count"] == nil ||
			fields["active_instrument_listing_count"] == nil || receiptFields["instrument_listing_check"] == nil ||
			receiptFields["candidate_instrument_listing_state_sha256"] == nil) {
			return ManifestPolicy{}, errors.New("legacy backup manifest omits required instrument listing fields")
		}
		if (policy.LegacyV5Manifest || policy.LegacyV6Manifest) && (fields["security_price_observation_state_sha256"] != nil || fields["security_price_observation_count"] != nil) {
			return ManifestPolicy{}, errors.New("legacy backup manifest contains current security price observation fields")
		}
		if (policy.LegacyV5Manifest || policy.LegacyV6Manifest) && (receiptFields["security_price_observation_check"] != nil || receiptFields["candidate_security_price_observation_state_sha256"] != nil) {
			return ManifestPolicy{}, errors.New("legacy backup receipt contains current security price observation fields")
		}
		if policy.LegacyV5Manifest && (fields["fx_observation_state_sha256"] != nil || fields["fx_observation_count"] != nil ||
			receiptFields["fx_observation_check"] != nil || receiptFields["candidate_fx_observation_state_sha256"] != nil) {
			return ManifestPolicy{}, errors.New("v5 backup manifest contains v6 FX observation fields")
		}
		if policy.LegacyV6Manifest && (fields["fx_observation_state_sha256"] == nil || fields["fx_observation_count"] == nil ||
			receiptFields["fx_observation_check"] == nil || receiptFields["candidate_fx_observation_state_sha256"] == nil) {
			return ManifestPolicy{}, errors.New("v6 backup manifest omits required FX observation fields")
		}
	}
	receipt := manifest.VerificationReceipt
	if err := domain.ValidateActivation(policy, domain.ActivationReceipt{
		Status: receipt.Status, IntegrityCheck: receipt.IntegrityCheck, GoldenSnapshotCheck: receipt.GoldenSnapshotCheck,
		OrderStateCheck: receipt.OrderStateCheck, BrokerStateCheck: receipt.BrokerStateCheck, StrategyRegistryCheck: receipt.StrategyRegistryCheck,
		FXObservationCheck: receipt.FXObservationCheck, SecurityPriceObservationCheck: receipt.SecurityPriceObservationCheck, InstrumentListingCheck: receipt.InstrumentListingCheck,
		PaperPerformanceCheck: receipt.PaperPerformanceCheck, PaperStrategyPerformanceCheck: receipt.PaperStrategyPerformanceCheck, PaperPerformancePolicyCheck: receipt.PaperPerformancePolicyCheck,
		PaperRunnerLeaseCheck: receipt.PaperRunnerLeaseCheck, EligibleForActivation: receipt.EligibleForActivation, ErrorCount: len(receipt.Errors),
	}); err != nil {
		return ManifestPolicy{}, err
	}
	return policy, nil
}
