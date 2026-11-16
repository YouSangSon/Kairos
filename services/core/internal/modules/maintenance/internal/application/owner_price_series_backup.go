package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
)

func RequireOwnerPriceManifestFields(fields, receipt map[string]json.RawMessage, current bool) error {
	for _, group := range []struct {
		fields map[string]json.RawMessage
		keys   []string
	}{
		{fields, []string{"owner_price_series_state_sha256", "owner_price_series_version_count", "owner_price_series_point_count"}},
		{receipt, []string{"owner_price_series_check", "candidate_owner_price_series_state_sha256", "candidate_owner_price_series_version_count", "candidate_owner_price_series_point_count"}},
	} {
		for _, key := range group.keys {
			value, present := group.fields[key]
			if current && (!present || bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
				return errors.New("backup omits owner price series proof")
			}
			if !current && present {
				return errors.New("legacy backup cannot claim owner price series proof")
			}
		}
	}
	return nil
}

func OwnerPriceManifestMatches(manifest contracts.BackupManifest, proof SeriesProof) bool {
	r := manifest.VerificationReceipt
	return manifest.OwnerPriceSeriesStateSHA256 == proof.SHA256 && manifest.OwnerPriceSeriesVersionCount == proof.Versions && manifest.OwnerPriceSeriesPointCount == proof.Points &&
		r.OwnerPriceSeriesCheck == "ok" && r.CandidateOwnerPriceSeriesStateSHA256 == proof.SHA256 && r.CandidateOwnerPriceSeriesVersionCount == proof.Versions && r.CandidateOwnerPriceSeriesPointCount == proof.Points
}
