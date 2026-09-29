package hp2

import (
	"context"
	"errors"
	"sync"

	"github.com/coder/websocket"
)

// FrameOverhead is what sealing adds to a plaintext frame (Poly1305 tag).
const FrameOverhead = 16

// Conn seals and opens the frames of one WebSocket connection (v2): only
// binary frames, each sealed with the next counter of its direction.
type Conn struct {
	ws     *websocket.Conn
	sealer *Sealer
	opener *Opener
	// writeMu keeps sealing order and wire order the same.
	writeMu sync.Mutex
}

// NewConn wraps ws; sendKey seals what this side writes, recvKey opens what
// it reads.
func NewConn(ws *websocket.Conn, sendKey, recvKey []byte) (*Conn, error) {
	sealer, err := NewSealer(sendKey)
	if err != nil {
		return nil, err
	}
	opener, err := NewOpener(recvKey)
	if err != nil {
		return nil, err
	}
	return &Conn{ws: ws, sealer: sealer, opener: opener}, nil
}

// WS returns the underlying connection (ping, close).
func (c *Conn) WS() *websocket.Conn { return c.ws }

// WriteJSON writes a JSON message (type byte 0x00).
func (c *Conn) WriteJSON(ctx context.Context, data []byte) error {
	plaintext := make([]byte, 0, 1+len(data))
	plaintext = append(plaintext, FrameJSON)
	return c.write(ctx, append(plaintext, data...))
}

// WriteAudio writes an audio frame in the v1.1 format (type byte 0x01 and
// 160 bytes A-law).
func (c *Conn) WriteAudio(ctx context.Context, frame []byte) error {
	if len(frame) == 0 || frame[0] != FrameAudio {
		return errors.New("hp2: audio frame must start with type 0x01")
	}
	return c.write(ctx, frame)
}

func (c *Conn) write(ctx context.Context, plaintext []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	sealed, err := c.sealer.Seal(plaintext)
	if err != nil {
		return err
	}
	return c.ws.Write(ctx, websocket.MessageBinary, sealed)
}

// Read returns the next opened frame: its type byte and the whole
// plaintext including the type byte (so audio frames keep their v1.1
// format). Text frames, frames that fail authentication or arrive out of
// order and empty frames return ErrIntegrity; the connection must then be
// closed with protocol.CloseIntegrity. Other read errors are returned as
// is.
func (c *Conn) Read(ctx context.Context) (byte, []byte, error) {
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	if typ != websocket.MessageBinary {
		return 0, nil, ErrIntegrity
	}
	plaintext, err := c.opener.Open(data)
	if err != nil {
		return 0, nil, err
	}
	if len(plaintext) == 0 {
		return 0, nil, ErrIntegrity
	}
	return plaintext[0], plaintext, nil
}
