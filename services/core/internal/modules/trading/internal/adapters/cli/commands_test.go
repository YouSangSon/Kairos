package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"omni-folio/services/core/internal/modules/trading/construction"
	"omni-folio/services/core/internal/modules/trading/contracts"
)

type sessionStub struct {
	service   contracts.Trading
	schemaErr error
	closed    int
}

func (s *sessionStub) Trading() contracts.Trading { return s.service }
func (s *sessionStub) RequireSchema() error       { return s.schemaErr }
func (s *sessionStub) Close() error {
	s.closed++
	return errors.New("close must not hide command error")
}

type resourcesStub struct {
	session *sessionStub
	opens   int
	mode    string
}

func (r *resourcesStub) OpenExisting(string) (construction.Session, error) {
	r.opens++
	r.mode = "existing"
	return r.session, nil
}
func (r *resourcesStub) OpenReadOnly(context.Context, string) (construction.Session, error) {
	r.opens++
	r.mode = "read-only"
	return r.session, nil
}
func (r *resourcesStub) OpenReadWrite(context.Context, string) (construction.Session, error) {
	r.opens++
	r.mode = "read-write"
	return r.session, nil
}

type canceledImprovement struct{ contracts.Trading }

func (canceledImprovement) ImprovePaperCandidate(context.Context, string, string, []byte, []byte) (*contracts.PaperImprovementReceipt, error) {
	return nil, context.Canceled
}

func TestInvalidFlagsAndHelpDoNotOpenTradingResources(t *testing.T) {
	commands := []string{"strategy-register", "strategy-status", "strategy-select", "strategy-rollback", "paper-run-due", "paper-run-loop", "paper-init", "paper-import-bars", "paper-execute", "paper-improve", "paper-input-status", "paper-promote", "paper-promotion-status", "paper-promotion-abort", "paper-execute-stream"}
	for _, command := range commands {
		for _, option := range []string{"-h", "-invalid-option"} {
			t.Run(command+option, func(t *testing.T) {
				input, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				defer writer.Close()
				resources := &resourcesStub{}
				if err := Run(context.Background(), command, []string{option}, resources, input, io.Discard); err == nil {
					t.Fatal("invalid/help unexpectedly succeeded")
				}
				if resources.opens != 0 {
					t.Fatalf("flags opened %d resources", resources.opens)
				}
			})
		}
	}
}

func TestSchemaFailureClosesTradingResourceWithoutHidingCause(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifact, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := errors.New("schema marker")
	session := &sessionStub{schemaErr: marker}
	resources := &resourcesStub{session: session}
	err := Run(context.Background(), "strategy-register", []string{"-artifact", artifact}, resources, nil, io.Discard)
	if err != marker || session.closed != 1 || resources.mode != "existing" {
		t.Fatalf("err=%v closed=%d mode=%s", err, session.closed, resources.mode)
	}
}

func TestCanceledImprovementClosesReadWriteResourceAndPreservesCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-input")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	session := &sessionStub{service: canceledImprovement{}}
	resources := &resourcesStub{session: session}
	err := Run(context.Background(), "paper-improve", []string{"-db", "test.db", "-account", "synthetic-account", "-artifact", path, "-bars", path, "-attempt-id", "synthetic-attempt"}, resources, nil, io.Discard)
	if !errors.Is(err, context.Canceled) || session.closed != 1 || resources.mode != "read-write" {
		t.Fatalf("err=%v closed=%d mode=%s", err, session.closed, resources.mode)
	}
}
