package composition

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func lifecycleListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func TestServeHTTPDrainsBeforeClosingRuntime(t *testing.T) {
	runtime, _, _ := migratedRuntime(t)
	listener := lifecycleListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, shutdown := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		runtime.Routes().ServeHTTP(w, r)
	})}
	server.RegisterOnShutdown(func() { close(shutdown) })
	t.Cleanup(func() { server.Close() })
	returned := make(chan error, 1)
	go func() {
		err := serveHTTP(ctx, server, listener, time.Second)
		runtime.Close()
		returned <- err
	}()
	response := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://" + listener.Addr().String() + "/readyz")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				err = errors.New("request lost runtime before drain")
			}
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case <-shutdown:
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation did not start HTTP shutdown")
	}
	select {
	case err := <-returned:
		t.Fatalf("serve returned before active request drained: %v", err)
	default:
	}
	late := httptest.NewRecorder()
	server.Handler.ServeHTTP(late, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutdown admitted a new handler: %d", late.Code)
	}
	if err := runtime.db.Ping(); err != nil {
		t.Fatalf("runtime closed before handler completed: %v", err)
	}
	release <- struct{}{}
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("graceful cancellation returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drained server did not return")
	}
	if err := runtime.db.Ping(); err == nil {
		t.Fatal("runtime remained open after drain")
	}
}

func TestServeHTTPTimeoutCancelsAndWaitsForHandler(t *testing.T) {
	runtime, _, _ := migratedRuntime(t)
	listener := lifecycleListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-release
		if err := runtime.db.Ping(); err != nil {
			t.Errorf("runtime closed while forced handler still used it: %v", err)
		}
	})}
	t.Cleanup(func() { server.Close() })
	returned := make(chan error, 1)
	go func() {
		err := serveHTTP(ctx, server, listener, 20*time.Millisecond)
		runtime.Close()
		returned <- err
	}()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		client := &http.Client{Timeout: 3 * time.Second}
		if response, err := client.Get("http://" + listener.Addr().String()); err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("grace timeout did not cancel active request")
	}
	select {
	case err := <-returned:
		t.Fatalf("forced server returned before handler completed: %v", err)
	default:
	}
	release <- struct{}{}
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("forced shutdown hid grace timeout: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("forced server did not return after handler completed")
	}
	<-requestDone
}

func TestServeCLIListenFailurePreservesDatabase(t *testing.T) {
	_, path, _ := migratedRuntime(t)
	listener := lifecycleListener(t)
	err := RunCLI(context.Background(), []string{"serve", "-db", path, "-addr", listener.Addr().String()}, CLIOptions{}, os.Stdin, io.Discard, io.Discard)
	var operation *net.OpError
	if !errors.As(err, &operation) || operation.Op != "listen" {
		t.Fatalf("occupied listener did not fail at startup: %v", err)
	}
	if err := RunCLI(context.Background(), []string{"migrate", "-db", path}, CLIOptions{}, os.Stdin, io.Discard, io.Discard); err != nil {
		t.Fatalf("failed startup left database unusable: %v", err)
	}
}
