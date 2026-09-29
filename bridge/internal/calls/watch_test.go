package calls

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Tests for the websocket-pcma media path of the watch (signaling v1.1).

func watchDevice(id, token string) store.Device {
	d := device(id, token)
	d.Platform = protocol.PlatformWatchOS
	d.MediaCapabilities = []string{protocol.MediaWebSocketPCMA}
	d.PushTopic = "com.jorisconrad.housephone.watchkitapp.voip"
	return d
}

// audioFrame builds a binary frame from the device: 0x01 + 160 bytes.
func audioFrame(fill byte) []byte {
	f := make([]byte, protocol.AudioFrameLen)
	f[0] = protocol.AudioFrameType
	for i := 1; i < len(f); i++ {
		f[i] = fill
	}
	return f
}

func pcmaPacket(seq uint16, payload []byte) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: seq, Timestamp: uint32(seq) * 160, SSRC: 4321}, Payload: payload}
}

func (f *fakeConn) expectAudio(t *testing.T) []byte {
	t.Helper()
	select {
	case frame := <-f.audio:
		return frame
	case <-time.After(waitTimeout):
		t.Fatalf("%s: no audio frame", f.id)
	}
	return nil
}

func (m *fakeMedia) expectPacket(t *testing.T) *rtp.Packet {
	t.Helper()
	select {
	case p := <-m.out:
		return p
	case <-time.After(waitTimeout):
		t.Fatal("no RTP packet towards the FRITZ!Box")
	}
	return nil
}

func (m *fakeMedia) expectNoPacket(t *testing.T) {
	t.Helper()
	select {
	case p := <-m.out:
		t.Fatalf("unexpected RTP packet %+v", p.Header)
	case <-time.After(60 * time.Millisecond):
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWatchAcceptsIncomingCallWithPCMA(t *testing.T) {
	h := newHarness(t, device("phone", "tok-p"), watchDevice("watch", "tok-w"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	h.expectPush(t)

	phone, watch := newFakeConn("phone"), newFakeConn("watch")
	h.m.DeviceConnected(phone)
	h.m.DeviceConnected(watch)
	offer := h.attachAndAnswer(t, phone, callID, "answer")
	if !bytes.Contains([]byte(offer.SDP), []byte("[G722]")) {
		t.Fatalf("the iPhone keeps getting the HD codec: %s", offer.SDP)
	}

	// The watch gets call.media instead of an offer and no WebRTC peer.
	h.send(watch, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	watch.expect(t, protocol.TypeCallIncoming, nil)
	var media protocol.CallMedia
	watch.expect(t, protocol.TypeCallMedia, &media)
	if media != protocol.NewCallMedia(callID) {
		t.Fatalf("call.media %+v", media)
	}
	h.peers.mu.Lock()
	peers := len(h.peers.peers)
	h.peers.mu.Unlock()
	if peers != 1 {
		t.Fatalf("%d WebRTC peers, want 1 (only the iPhone)", peers)
	}

	// A call.answer from the watch is an error, but the watch stays in.
	h.send(watch, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: "v=0"})
	var callErr protocol.Error
	watch.expect(t, protocol.TypeError, &callErr)
	if callErr.Code != protocol.ErrorBadRequest {
		t.Fatalf("error %+v", callErr)
	}

	h.send(watch, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	sip.expectEvent(t, "200")
	if got := sip.answerCodec(); got != codec.PCMA {
		t.Fatalf("200 OK with %s, want PCMA for the watch", got)
	}
	phone.expectEnded(t, protocol.EndReasonAnsweredElsewhere)
	var st protocol.CallState
	watch.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state %q", st.State)
	}
	eventually(t, "iPhone peer closed", h.peers.peer(t, 0).isClosed)

	// FRITZ!Box → watch: packets of any size become 20 ms frames;
	// telephone-event is not audio.
	sip.media.in <- rtpPacket(101, 1)
	sip.media.in <- pcmaPacket(2, bytes.Repeat([]byte{0x11}, 240))
	sip.media.in <- pcmaPacket(3, bytes.Repeat([]byte{0x22}, 80))
	first, second := watch.expectAudio(t), watch.expectAudio(t)
	if !bytes.Equal(first, audioFrame(0x11)) {
		t.Fatalf("first frame % x", first[:4])
	}
	want := append([]byte{protocol.AudioFrameType}, append(bytes.Repeat([]byte{0x11}, 80), bytes.Repeat([]byte{0x22}, 80)...)...)
	if !bytes.Equal(second, want) {
		t.Fatal("second frame does not continue the byte stream")
	}

	// Watch → FRITZ!Box: paced RTP with PCMA, one SSRC, consecutive
	// sequence numbers and timestamps in steps of 160.
	watch.deviceAudio([]byte{0x02, 0x00})     // unknown type: ignored
	watch.deviceAudio(audioFrame(0xAA)[:100]) // wrong length: ignored
	for _, fill := range []byte{0xA1, 0xA2, 0xA3} {
		watch.deviceAudio(audioFrame(fill))
	}
	var prev *rtp.Packet
	for i, fill := range []byte{0xA1, 0xA2, 0xA3} {
		p := sip.media.expectPacket(t)
		if p.PayloadType != 8 || !bytes.Equal(p.Payload, bytes.Repeat([]byte{fill}, 160)) {
			t.Fatalf("packet %d: pt=%d payload % x", i, p.PayloadType, p.Payload[:2])
		}
		if prev != nil {
			if p.SSRC != prev.SSRC || p.SequenceNumber != prev.SequenceNumber+1 {
				t.Fatalf("packet %d: ssrc/seq %d/%d after %d/%d", i, p.SSRC, p.SequenceNumber, prev.SSRC, prev.SequenceNumber)
			}
			if step := p.Timestamp - prev.Timestamp; step == 0 || step%160 != 0 {
				t.Fatalf("packet %d: timestamp step %d", i, step)
			}
		}
		prev = p
	}

	h.send(watch, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID, Reason: protocol.HangupReasonHangup})
	watch.expectEnded(t, protocol.EndReasonLocalHangup)
	sip.expectEvent(t, "BYE")
	waitClosed(t, done, "incoming call")
	eventually(t, "audio sink removed", func() bool { return !watch.hasSink() })
}

func TestPhoneAcceptsWhileWatchRings(t *testing.T) {
	h := newHarness(t, device("phone", "tok-p"), watchDevice("watch", "tok-w"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	h.expectPush(t)

	phone, watch := newFakeConn("phone"), newFakeConn("watch")
	h.m.DeviceConnected(phone)
	h.m.DeviceConnected(watch)
	h.send(watch, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	watch.expect(t, protocol.TypeCallIncoming, nil)
	watch.expect(t, protocol.TypeCallMedia, nil)
	if !watch.hasSink() {
		t.Fatal("ringing watch should already route its audio to the call")
	}
	h.attachAndAnswer(t, phone, callID, "answer")
	h.send(phone, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	sip.expectEvent(t, "200")
	if got := sip.answerCodec(); got != codec.G722 {
		t.Fatalf("200 OK with %s, want G722 for the iPhone", got)
	}
	watch.expectEnded(t, protocol.EndReasonAnsweredElsewhere)
	eventually(t, "watch sink removed", func() bool { return !watch.hasSink() })
	phone.expect(t, protocol.TypeCallState, nil)

	sip.remoteEnd(SIPEndRemoteHangup)
	phone.expectEnded(t, protocol.EndReasonRemoteHangup)
	waitClosed(t, done, "incoming call")
}

func TestWatchIsNotRungWithoutPCMA(t *testing.T) {
	// Only the watch is paired: nothing can ring → 480.
	h := newHarness(t, watchDevice("watch", "tok-w"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	sip.offered = []codec.Codec{codec.G722}
	done := h.incoming(sip)
	sip.expectEvent(t, "480")
	waitClosed(t, done, "incoming call")
	select {
	case p := <-h.pusher.pushes:
		t.Fatalf("watch was pushed: %+v", p)
	default:
	}

	// With the iPhone: only the iPhone rings; a stray attach from the watch
	// ends for the watch with 488.
	h = newHarness(t, device("phone", "tok-p"), watchDevice("watch", "tok-w"))
	sip = newFakeSIPIn("+4930123456", codec.G722)
	sip.offered = []codec.Codec{codec.G722}
	done = h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	select {
	case p := <-h.pusher.pushes:
		t.Fatalf("second push: %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
	watch := newFakeConn("watch")
	h.m.DeviceConnected(watch)
	h.send(watch, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	ended := watch.expectEnded(t, protocol.EndReasonFailed)
	if ended.SIPCode != 488 {
		t.Fatalf("sipCode %d", ended.SIPCode)
	}
	h.send(watch, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	watch.expectNothing(t)
	sip.remoteEnd(SIPEndCancelled)
	waitClosed(t, done, "incoming call")
}

func TestOutgoingCallFromWatch(t *testing.T) {
	h := newHarness(t, watchDevice("watch", ""))
	out := newFakeSIPOut()
	media := newFakeMedia()
	answerGate := make(chan struct{})
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		ev.OnRinging()
		ev.OnEarlyMedia(media)
		<-answerGate
		return out, media, nil
	}
	watch := newFakeConn("watch")
	h.m.DeviceConnected(watch)
	callID := uuid.NewString()
	h.send(watch, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "+4930123456"})

	watch.expect(t, protocol.TypeCallMedia, nil)
	select {
	case d := <-h.sip.dials:
		if d != "+4930123456/PCMA" {
			t.Fatalf("dialed %q, want PCMA only", d)
		}
	case <-time.After(waitTimeout):
		t.Fatal("no dial: the watch has no answer to wait for")
	}
	var st protocol.CallState
	watch.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateRinging {
		t.Fatalf("state %q", st.State)
	}
	watch.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateEarlyMedia {
		t.Fatalf("state %q", st.State)
	}

	// Early media reaches the watch; the watch's audio is dropped until
	// the call is connected.
	media.in <- pcmaPacket(1, bytes.Repeat([]byte{0x33}, 160))
	if frame := watch.expectAudio(t); !bytes.Equal(frame, audioFrame(0x33)) {
		t.Fatal("early media frame mismatch")
	}
	watch.deviceAudio(audioFrame(0x44))
	media.expectNoPacket(t)

	close(answerGate)
	watch.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state %q", st.State)
	}
	watch.deviceAudio(audioFrame(0x55))
	if p := media.expectPacket(t); p.PayloadType != 8 || p.Payload[0] != 0x55 {
		t.Fatalf("packet %+v", p.Header)
	}

	// Re-attach on a new connection: call.media again, audio continues there.
	h.m.DeviceDisconnected(watch)
	watch2 := newFakeConn("watch")
	h.m.DeviceConnected(watch2)
	h.send(watch2, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	watch2.expect(t, protocol.TypeCallMedia, nil)
	watch2.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state after re-attach %q", st.State)
	}
	if watch.hasSink() || !watch2.hasSink() {
		t.Fatal("audio sink should move to the new connection")
	}
	watch2.deviceAudio(audioFrame(0x66))
	if p := media.expectPacket(t); p.Payload[0] != 0x66 {
		t.Fatal("audio from the new connection not relayed")
	}
	media.in <- pcmaPacket(2, bytes.Repeat([]byte{0x77}, 160))
	if frame := watch2.expectAudio(t); frame[1] != 0x77 {
		t.Fatal("SIP audio not sent to the new connection")
	}

	h.send(watch2, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID})
	watch2.expectEnded(t, protocol.EndReasonLocalHangup)
	select {
	case <-out.hangups:
	case <-time.After(waitTimeout):
		t.Fatal("no BYE")
	}
}

func TestWSPeerQueueIsBoundedAndPaced(t *testing.T) {
	p := newWSPeer(10 * time.Millisecond)
	defer p.Close()
	// Before the call is connected, device audio is dropped.
	p.DeviceAudio(audioFrame(1))
	p.setLive(true)
	for i := 0; i < wsQueueFrames+5; i++ {
		p.DeviceAudio(audioFrame(byte(i)))
	}
	if got := p.droppedFrames(); got != 5 {
		t.Fatalf("dropped %d, want 5", got)
	}
	start := time.Now()
	first, err := p.ReadRTP()
	if err != nil {
		t.Fatal(err)
	}
	if first.Payload[0] != 5 {
		t.Fatalf("oldest frames should be dropped, got %d first", first.Payload[0])
	}
	if !first.Marker {
		t.Fatal("first packet of a talkspurt needs the marker bit")
	}
	for i := 1; i < wsQueueFrames; i++ {
		pkt, err := p.ReadRTP()
		if err != nil {
			t.Fatal(err)
		}
		if pkt.Marker || pkt.SequenceNumber != first.SequenceNumber+uint16(i) || pkt.Timestamp != first.Timestamp+uint32(i)*160 {
			t.Fatalf("packet %d: seq %d ts %d marker %v", i, pkt.SequenceNumber, pkt.Timestamp, pkt.Marker)
		}
	}
	// 10 frames on a 10 ms clock take about 100 ms, not a burst.
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("frames were not paced: %v", elapsed)
	}
	_ = p.Close()
	if _, err := p.ReadRTP(); err == nil {
		t.Fatal("ReadRTP after Close should fail")
	}
}
