package calls

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Tests for the WebSocket audio fallback of WebRTC devices (signaling v1.4).

// fallbackPhone is an iPhone that also takes websocket-pcma audio.
func fallbackPhone(id string) store.Device {
	d := device(id, "")
	d.MediaCapabilities = []string{protocol.MediaWebRTC, protocol.MediaWebSocketPCMA}
	return d
}

func newFallbackHarness(t *testing.T, devices ...store.Device) *harness {
	h := newHarness(t, devices...)
	h.m.opts.MediaFallbackTimeout = 80 * time.Millisecond
	return h
}

// dialConnected dials from conn and returns the SIP media once the call
// is connected; the offer is answered with answerSDP.
func dialConnected(t *testing.T, h *harness, conn *fakeConn, answerSDP string) (callID string, offer protocol.CallOffer, media *fakeMedia, dialed string) {
	t.Helper()
	media = newFakeMedia()
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		return newFakeSIPOut(), media, nil
	}
	h.m.DeviceConnected(conn)
	callID = uuid.NewString()
	h.send(conn, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "+4930123456"})
	conn.expect(t, protocol.TypeCallOffer, &offer)
	h.send(conn, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: answerSDP})
	select {
	case dialed = <-h.sip.dials:
	case <-time.After(waitTimeout):
		t.Fatal("no dial")
	}
	var st protocol.CallState
	conn.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state %q", st.State)
	}
	return callID, offer, media, dialed
}

func TestRemoteOutgoingCallFallsBackToWebSocket(t *testing.T) {
	h := newFallbackHarness(t, fallbackPhone("phone"))
	phone := newFakeConn("phone")
	phone.remote = true
	callID, offer, media, dialed := dialConnected(t, h, phone, "answer")

	// Away from home only PCMA is offered, so the fallback needs no transcoding.
	if !strings.Contains(offer.SDP, "[PCMA]") {
		t.Fatalf("remote offer should be PCMA only: %s", offer.SDP)
	}
	if dialed != "+4930123456/PCMA" {
		t.Fatalf("dialed %q", dialed)
	}

	// The WebRTC path never connects: call.media follows.
	var cm protocol.CallMedia
	phone.expect(t, protocol.TypeCallMedia, &cm)
	if cm.CallID != callID || cm.Transport != protocol.MediaTransportWebSocket || cm.Codec != "PCMA" {
		t.Fatalf("call.media %+v", cm)
	}
	var st protocol.CallState
	phone.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state after fallback %q", st.State)
	}
	if !h.peers.peer(t, 0).isClosed() {
		t.Fatal("the WebRTC peer should be closed")
	}

	// Audio now runs over the WebSocket in both directions.
	eventually(t, "audio sink on the connection", phone.hasSink)
	phone.deviceAudio(audioFrame(0x55))
	if p := media.expectPacket(t); p.PayloadType != 8 || p.Payload[0] != 0x55 {
		t.Fatalf("packet %+v", p.Header)
	}
	// The stopped WebRTC relay may still swallow one packet (20 ms).
	media.in <- pcmaPacket(1, bytes.Repeat([]byte{0x33}, 160))
	media.in <- pcmaPacket(2, bytes.Repeat([]byte{0x33}, 160))
	if frame := phone.expectAudio(t); !bytes.Equal(frame, audioFrame(0x33)) {
		t.Fatal("SIP audio not sent over the WebSocket")
	}

	// A re-attach keeps the WebSocket path.
	h.m.DeviceDisconnected(phone)
	phone2 := newFakeConn("phone")
	phone2.remote = true
	h.m.DeviceConnected(phone2)
	h.send(phone2, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	phone2.expect(t, protocol.TypeCallMedia, nil)
	phone2.expect(t, protocol.TypeCallState, nil)

	h.send(phone2, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID})
	phone2.expectEnded(t, protocol.EndReasonLocalHangup)
}

func TestConnectedWebRTCDoesNotFallBack(t *testing.T) {
	h := newFallbackHarness(t, fallbackPhone("phone"))
	phone := newFakeConn("phone")
	phone.remote = true
	callID, _, _, _ := dialConnected(t, h, phone, "answer")
	h.peers.peer(t, 0).onState(PeerConnected)

	time.Sleep(3 * h.m.opts.MediaFallbackTimeout)
	phone.expectNothing(t)
	h.send(phone, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID})
	phone.expectEnded(t, protocol.EndReasonLocalHangup)
}

func TestHomeCallKeepsHDVoiceWithoutFallback(t *testing.T) {
	h := newFallbackHarness(t, fallbackPhone("phone"))
	phone := newFakeConn("phone") // home network
	callID, offer, _, dialed := dialConnected(t, h, phone, "codec:G722")
	if !strings.Contains(offer.SDP, "[G722 PCMA PCMU]") || dialed != "+4930123456/G722" {
		t.Fatalf("home call should keep all codecs: %s, dialed %q", offer.SDP, dialed)
	}
	// G.722 cannot go over the WebSocket, so a failure stays an ICE restart.
	h.peers.peer(t, 0).onState(PeerFailed)
	var again protocol.CallOffer
	phone.expect(t, protocol.TypeCallOffer, &again)
	phone.expect(t, protocol.TypeCallState, nil)
	h.send(phone, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID})
	phone.expectEnded(t, protocol.EndReasonLocalHangup)
}

func TestIncomingCallFallsBackWhenICEFails(t *testing.T) {
	dev := fallbackPhone("phone")
	dev.PushToken = "tok-p"
	h := newFallbackHarness(t, dev)
	sip := newFakeSIPIn("+4930999", codec.PCMA)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID

	phone := newFakeConn("phone")
	phone.remote = true
	h.m.DeviceConnected(phone)
	h.attachAndAnswer(t, phone, callID, "answer")

	// ICE fails while it rings: the device gets WebSocket audio instead.
	h.peers.peer(t, 0).onState(PeerFailed)
	phone.expect(t, protocol.TypeCallMedia, nil)

	h.send(phone, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	sip.expectEvent(t, "200")
	if got := sip.answerCodec(); got != codec.PCMA {
		t.Fatalf("answered with %s", got)
	}
	var st protocol.CallState
	phone.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state %q", st.State)
	}
	eventually(t, "audio sink on the connection", phone.hasSink)
	phone.deviceAudio(audioFrame(0x42))
	if p := sip.media.expectPacket(t); p.Payload[0] != 0x42 {
		t.Fatal("device audio not relayed after the fallback")
	}
	sip.remoteEnd(SIPEndRemoteHangup)
	phone.expectEnded(t, protocol.EndReasonRemoteHangup)
	<-done
}
