package cli

import (
	"context"
	"errors"
	"io"
	"omni-folio/services/core/internal/modules/maintenance/construction"
	"testing"
)

type resourcesStub struct{ opens, verifications int }

func (r *resourcesStub) Open(string) (construction.Session, error) {
	r.opens++
	return nil, errors.New("unexpected open")
}
func (r *resourcesStub) OpenExisting(string) (construction.Session, error) {
	r.opens++
	return nil, errors.New("unexpected open")
}
func (r *resourcesStub) VerifyManifest(context.Context, string, string, string) error {
	r.verifications++
	return errors.New("manifest failed before opening candidate")
}
func TestMaintenanceCLIValidatesFlagsBeforeResources(t *testing.T) {
	for _, command := range []string{"migrate", "backup", "verify-restore"} {
		for _, arg := range []string{"-h", "-invalid"} {
			r := &resourcesStub{}
			if err := Run(context.Background(), command, []string{arg}, r, io.Discard); err == nil || r.opens != 0 || r.verifications != 0 {
				t.Fatalf("%s %s touched resources: %v %+v", command, arg, err, r)
			}
		}
	}
	for _, command := range []string{"backup", "verify-restore"} {
		r := &resourcesStub{}
		if err := Run(context.Background(), command, nil, r, io.Discard); err == nil || r.opens != 0 || r.verifications != 0 {
			t.Fatalf("%s accepted absent paths: %v", command, err)
		}
	}
}
func TestVerifyRestoreDelegatesWithoutOpeningSource(t *testing.T) {
	r := &resourcesStub{}
	err := Run(context.Background(), "verify-restore", []string{"-db", "candidate", "-golden", "golden", "-manifest", "manifest"}, r, io.Discard)
	if err == nil || err.Error() != "manifest failed before opening candidate" || r.opens != 0 || r.verifications != 1 {
		t.Fatalf("err=%v resources=%+v", err, r)
	}
}
