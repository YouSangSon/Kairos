package contracts

import "context"

type ManifestVerifier interface {
	VerifyManifest(context.Context, string, string, string) error
}
