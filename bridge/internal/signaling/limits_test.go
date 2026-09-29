package signaling

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Without HP2 authentication a WebSocket is never upgraded (signaling v2),
// so no unauthenticated connection can idle and occupy resources: many
// attempts in parallel all get 401 at once, and a paired device still
// connects.
func TestUnauthenticatedConnectionsNeverOpen(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)

	results := make(chan int, 50)
	for range 50 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, res, err := websocket.Dial(ctx, ts.wsURL(), nil)
			if err == nil {
				c.CloseNow()
				results <- http.StatusSwitchingProtocols
				return
			}
			if res == nil {
				results <- 0
				return
			}
			results <- res.StatusCode
		}()
	}
	for range 50 {
		if status := <-results; status != http.StatusUnauthorized {
			t.Fatalf("unauthenticated dial got %d, want 401", status)
		}
	}

	c, err := ts.dial(t, d)
	if err != nil {
		t.Fatalf("authenticated connection refused: %v", err)
	}
	c.WS().CloseNow()
}
