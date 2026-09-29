package signaling

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Unauthenticated WebSocket connections are bounded: each may idle for
// FirstMessageWait before sending pair.
func TestUnauthenticatedConnectionsAreBounded(t *testing.T) {
	ts := newTestServer(t, func(c *Config) { c.FirstMessageWait = 3 * time.Second })
	phone := ts.pairDevice(t)

	var conns []*websocket.Conn
	defer func() {
		for _, c := range conns {
			c.CloseNow()
		}
	}()
	for range maxPairingConns {
		c, _, err := ts.dial(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, res, err := websocket.Dial(ctx, ts.wsURL(), nil)
	if err == nil || res == nil || res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("connection beyond the limit: %v %v", res, err)
	}

	// Authenticated connections are not affected.
	c, _, err := ts.dial(t, bearer(phone))
	if err != nil {
		t.Fatalf("authenticated connection refused: %v", err)
	}
	c.CloseNow()
}
