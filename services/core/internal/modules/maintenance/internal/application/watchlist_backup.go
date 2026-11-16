package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/maintenance/contracts"
	watch "omni-folio/services/core/internal/modules/watchlist/contracts"
)

func RequireWatchlistManifestFields(fields, receipt map[string]json.RawMessage, current bool) error {
	for _, group := range []struct {
		fields map[string]json.RawMessage
		keys   []string
	}{
		{fields, []string{"watchlist_state_sha256", "watchlist_revision", "watchlist_entry_count"}},
		{receipt, []string{"watchlist_check", "candidate_watchlist_state_sha256", "candidate_watchlist_revision", "candidate_watchlist_entry_count"}},
	} {
		for _, key := range group.keys {
			value, present := group.fields[key]
			if current && (!present || bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
				return errors.New("backup omits watchlist proof")
			}
			if !current && present {
				return errors.New("legacy backup cannot claim watchlist proof")
			}
		}
	}
	return nil
}

func WatchlistManifestMatches(manifest contracts.BackupManifest, proof watch.RecoveryProof) bool {
	r := manifest.VerificationReceipt
	return manifest.WatchlistStateSHA256 == proof.SHA256 && manifest.WatchlistRevision == proof.Revision && manifest.WatchlistEntryCount == proof.Entries &&
		r.WatchlistCheck == "ok" && r.CandidateWatchlistStateSHA256 == proof.SHA256 && r.CandidateWatchlistRevision == proof.Revision && r.CandidateWatchlistEntryCount == proof.Entries
}
