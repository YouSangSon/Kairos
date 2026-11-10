package sqlite

import (
	"context"
	c "omni-folio/services/core/internal/modules/maintenance/contracts"
	"time"
)

type manifestVerifier struct{ store *Store }

func NewManifestVerifier(now func() time.Time, id func(string) string, factory OwnerFactory) c.ManifestVerifier {
	return &manifestVerifier{store: New(nil, nil, nil, now, id, factory)}
}
func (v *manifestVerifier) VerifyManifest(ctx context.Context, path, golden, manifest string) error {
	return v.store.VerifyManifest(ctx, path, golden, manifest)
}
