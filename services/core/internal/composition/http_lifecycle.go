package composition

import (
	"context"
	"net"
	"net/http"
	"time"
)

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, grace time.Duration) error {
	return server.Serve(listener)
}
