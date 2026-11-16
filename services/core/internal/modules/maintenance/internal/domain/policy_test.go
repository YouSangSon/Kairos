package domain

import "testing"

func TestManifestProfilesPreserveVersionMatrix(t *testing.T) {
	for _, tc := range []struct {
		format, schema                       string
		valid, watch, promotion, nonarchival bool
	}{
		{"omni-folio-backup.v21", "omni-folio.sqlite.v27", true, true, true, true},
		{"omni-folio-backup.v6", "omni-folio.sqlite.v10", true, false, false, false},
		{"omni-folio-backup.v7", "omni-folio.sqlite.v11", true, false, false, false},
		{"omni-folio-backup.v7", "omni-folio.sqlite.v12", true, false, false, false},
		{"omni-folio-backup.v8", "omni-folio.sqlite.v13", true, false, false, false},
		{"omni-folio-backup.v9", "omni-folio.sqlite.v14", true, false, false, false},
		{"omni-folio-backup.v10", "omni-folio.sqlite.v15", true, false, false, false},
		{"omni-folio-backup.v11", "omni-folio.sqlite.v17", true, false, false, false},
		{"omni-folio-backup.v12", "omni-folio.sqlite.v18", true, false, false, false},
		{"omni-folio-backup.v13", "omni-folio.sqlite.v19", true, false, false, false},
		{"omni-folio-backup.v14", "omni-folio.sqlite.v20", true, false, false, false},
		{"omni-folio-backup.v16", "omni-folio.sqlite.v22", true, false, false, true},
		{"omni-folio-backup.v17", "omni-folio.sqlite.v23", true, false, false, true},
		{"omni-folio-backup.v18", "omni-folio.sqlite.v24", true, false, false, true},
		{"omni-folio-backup.v19", "omni-folio.sqlite.v25", true, false, false, true},
		{"omni-folio-backup.v20", "omni-folio.sqlite.v26", true, true, false, true},
		{"omni-folio-backup.v15", "omni-folio.sqlite.v21", true, false, false, false},
		{"omni-folio-backup.v5", "omni-folio.sqlite.v8", true, false, false, false},
		{"omni-folio-backup.v5", "omni-folio.sqlite.v9", true, false, false, false},
		{"omni-folio-backup.v21", "omni-folio.sqlite.v26", false, false, false, false},
	} {
		policy, err := ClassifyManifest(tc.format, tc.schema, false, "none")
		if (err == nil) != tc.valid {
			t.Fatalf("profile %s/%s: %v", tc.format, tc.schema, err)
		}
		if tc.valid && (policy.HasWatchlistManifest != tc.watch || policy.CurrentV21Manifest != tc.promotion || policy.NonArchivalManifest != tc.nonarchival) {
			t.Fatalf("profile = %+v", policy)
		}
	}
	if _, err := ClassifyManifest("omni-folio-backup.v21", "omni-folio.sqlite.v27", true, "none"); err == nil {
		t.Fatal("encrypted manifest admitted")
	}
}
func TestActivationAndLegacyPolicies(t *testing.T) {
	if RunnerEligible(1) || !RunnerEligible(0) || RunnerEligible(-1) {
		t.Fatal("active runner rule changed")
	}
	if GoldenReplayPolicy("", "current", true) != "current" || GoldenReplayPolicy("", "current", false) != "" || GoldenReplayPolicy("old", "current", true) != "old" {
		t.Fatal("archival replay fallback changed")
	}
	if LegacyPromotionEligible(1, 0) || !LegacyPromotionEligible(0, 0) {
		t.Fatal("legacy promotion history admitted")
	}
}

func TestActivationRequiresOnlyVersionOwnedChecks(t *testing.T) {
	receipt := ActivationReceipt{Status: "verified", IntegrityCheck: "ok", GoldenSnapshotCheck: "ok", OrderStateCheck: "ok", BrokerStateCheck: "ok", StrategyRegistryCheck: "ok", EligibleForActivation: true}
	v5, err := ClassifyManifest("omni-folio-backup.v5", "omni-folio.sqlite.v8", false, "none")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateActivation(v5, receipt); err != nil {
		t.Fatalf("v5 acquired later checks: %v", err)
	}
	current, err := ClassifyManifest("omni-folio-backup.v21", "omni-folio.sqlite.v27", false, "none")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateActivation(current, receipt); err == nil {
		t.Fatal("current receipt omitted owned checks")
	}
	receipt.FXObservationCheck, receipt.SecurityPriceObservationCheck, receipt.InstrumentListingCheck = "ok", "ok", "ok"
	receipt.PaperPerformanceCheck, receipt.PaperStrategyPerformanceCheck, receipt.PaperPerformancePolicyCheck, receipt.PaperRunnerLeaseCheck = "ok", "ok", "ok", "ok"
	if err := ValidateActivation(current, receipt); err != nil {
		t.Fatal(err)
	}
	receipt.ErrorCount = 1
	if err := ValidateActivation(current, receipt); err == nil {
		t.Fatal("receipt with verification error admitted")
	}
}
