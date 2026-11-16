package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the shipped main, not a test helper's substitute signal handler.

func buildLocalPaperExecutable(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "omni-core")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	// Prove cold startup independently from input/termination liveness.
	startup, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	started := time.Now()
	output, err := exec.CommandContext(startup, bin).CombinedOutput()
	var exit *exec.ExitError
	if startup.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("usage:")) {
		t.Fatalf("executable startup failed: %v\n%s", err, output)
	}
	t.Logf("cold executable startup: %s", time.Since(started))
	return bin
}
