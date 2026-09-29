package sipleg_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// syncBuffer is a bytes.Buffer safe for concurrent log writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The log shows the codecs the FRITZ!Box offered but never a caller's full
// number or name (log.showNumbers is off by default).
func TestIncomingInviteLogsOfferedCodecsButNoNumber(t *testing.T) {
	var logs syncBuffer
	e := setupWithLogger(t, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	go func() { _, _, _ = e.box.Call(context.Background(), "0301234567", "Oma") }()
	call := e.nextIncoming(t)
	_ = call.Reject(486, "Busy Here")

	out := logs.String()
	if !strings.Contains(out, `msg="incoming INVITE"`) || !strings.Contains(out, "offered=") || !strings.Contains(out, "G722") {
		t.Fatalf("offered codecs not logged:\n%s", out)
	}
	if strings.Contains(out, "0301234567") || strings.Contains(out, "Oma") {
		t.Fatalf("log contains the caller's number or name:\n%s", out)
	}
	if !strings.Contains(out, "…567") {
		t.Fatalf("masked number missing:\n%s", out)
	}
}
