package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestServeMainSIGTERMExitsCleanlyAndReleasesPort(t *testing.T) {
	buildContext, stopBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer stopBuild()
	dir := t.TempDir()
	binary := filepath.Join(dir, "omni-core")
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, ".")
	build.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	build.Cancel = func() error { return syscall.Kill(-build.Process.Pid, syscall.SIGKILL) }
	build.WaitDelay = time.Second
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual main: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	path := filepath.Join(dir, "core.db")
	if output, err := exec.CommandContext(ctx, binary, "migrate", "-db", path).CombinedOutput(); err != nil {
		t.Fatalf("migrate actual main: %v\n%s", err, output)
	}
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reserved.Addr().String()
	reserved.Close()
	cmd := exec.CommandContext(ctx, binary, "serve", "-db", path, "-addr", addr)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			cmd.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	defer client.CloseIdleConnections()
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + addr + "/readyz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case err := <-done:
			finished = true
			t.Fatalf("actual main exited before readiness: %v\n%s", err, output.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("actual main never became ready")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatalf("serve SIGTERM did not exit 0: %v\n%s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual main did not exit after SIGTERM")
	}
	released, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("serve retained listening port after exit: %v", err)
	}
	released.Close()
}
