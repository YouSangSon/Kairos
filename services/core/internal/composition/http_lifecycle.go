package composition

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"omni-folio/services/core/internal/transport/httpjson"
	"sync"
	"time"
)

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, grace time.Duration) error {
	var admission sync.Mutex
	var active sync.WaitGroup
	stopping := false
	handler := server.Handler
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admission.Lock()
		if stopping {
			admission.Unlock()
			httpjson.Write(w, http.StatusServiceUnavailable, APIError{Code: "server_shutting_down", Message: "server is shutting down"})
			return
		}
		active.Add(1)
		admission.Unlock()
		defer active.Done()
		handler.ServeHTTP(w, r)
	})

	var serveErr error
	served := make(chan struct{})
	go func() {
		serveErr = server.Serve(listener)
		close(served)
	}()
	select {
	case <-ctx.Done():
	case <-served:
	}
	admission.Lock()
	stopping = true
	admission.Unlock()

	shutdown, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	var closeErr error
	if shutdownErr != nil {
		log.Printf("HTTP graceful shutdown failed; closing active connections: %v", shutdownErr)
		closeErr = server.Close()
		shutdownErr = fmt.Errorf("HTTP graceful shutdown: %w", shutdownErr)
	}
	<-served
	// Close cancels connections but does not wait for handlers that still use the DB.
	active.Wait()
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr, closeErr)
}
