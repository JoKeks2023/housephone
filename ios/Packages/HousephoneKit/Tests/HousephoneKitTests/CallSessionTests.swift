import Foundation
import Testing
@testable import HousephoneKit

struct CallSessionTests {
    let callId = CallID()
    let start = Date(timeIntervalSince1970: 1_800_000_000)

    func push(caller: String = "+4930123456", name: String? = nil) -> IncomingCallPush {
        IncomingCallPush(callId: callId, caller: caller, callerName: name, bridgeId: "b")
    }

    func offer() -> SessionOffer {
        SessionOffer(callId: callId, sdp: "v=0", iceServers: [])
    }

    // MARK: Incoming

    @Test func incomingHappyPath() {
        var (call, effects) = CallSession.incoming(push: push(), now: start)
        #expect(effects == [.sendAttach])
        #expect(call.phase == .waitingForBridge)

        #expect(call.handle(.bridgeConfirmedIncoming(IncomingCall(callId: callId, caller: "+4930123456", startedAt: start))) == [])
        #expect(call.phase == .ringing)

        #expect(call.handle(.offer(offer())) == [.negotiate(offer())])
        #expect(call.handle(.localAnswerSent) == [])
        #expect(call.handle(.userAnswered) == [.sendAccept])
        #expect(call.phase == .answering)

        #expect(call.handle(.remoteState(.connected), now: start + 2) == [])
        #expect(call.phase == .connected)

        #expect(call.handle(.bridgeEnded(.remoteHangup), now: start + 62) == [.closeMedia, .reportEnded(.remoteEnded)])
        #expect(call.outcome == .answered)
        #expect(call.duration == 60)
    }

    @Test func directModeAcceptsOnAnswer() {
        var (call, _) = CallSession.incoming(announced: IncomingCall(callId: callId, caller: "5550100", startedAt: start), now: start)
        #expect(call.handle(.directMedia) == [])
        #expect(call.mediaMode == .directRTP)
        #expect(call.handle(.userAnswered) == [.sendAccept])
        #expect(call.handle(.remoteState(.connected), now: start + 1) == [])
        #expect(call.phase == .connected)
    }

    @Test func answerBeforeBridgeContactSendsAcceptOnceAnswerIsOut() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        // The user taps "answer" on the lock screen before signaling is up.
        #expect(call.handle(.userAnswered) == [])
        #expect(call.phase == .answering)
        #expect(call.handle(.offer(offer())) == [.negotiate(offer())])
        #expect(call.phase == .answering)
        #expect(call.handle(.localAnswerSent) == [.sendAccept])
        // Never twice, e.g. after an ICE-restart answer.
        #expect(call.handle(.localAnswerSent) == [])
    }

    @Test func declineWhileRinging() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        _ = call.handle(.bridgeConfirmedIncoming(IncomingCall(callId: callId, caller: "+4930123456", startedAt: start)))
        #expect(call.handle(.userEnded) == [.sendHangup(.declined), .closeMedia])
        #expect(call.outcome == .declined)
        // The bridge's confirmation must not be reported to CallKit again.
        #expect(call.handle(.bridgeEnded(.localHangup)) == [])
    }

    @Test func callerCancelsBeforeAnswer() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        #expect(call.handle(.bridgeEnded(.remoteCancelled)) == [.closeMedia, .reportEnded(.unanswered)])
        #expect(call.outcome == .missed)
    }

    @Test func otherPhoneAnswered() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        #expect(call.handle(.bridgeEnded(.answeredElsewhere)) == [.closeMedia, .reportEnded(.answeredElsewhere)])
        #expect(call.outcome == .answeredElsewhere)
    }

    @Test func attachTimeoutFailsOnlyWithoutBridgeContact() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        #expect(call.handle(.attachTimedOut) == [.closeMedia, .reportEnded(.failed)])
        #expect(call.outcome == .missed)

        var (confirmed, _) = CallSession.incoming(push: push(), now: start)
        _ = confirmed.handle(.bridgeConfirmedIncoming(IncomingCall(callId: callId, caller: "1", startedAt: start)))
        #expect(confirmed.handle(.attachTimedOut) == [])
        #expect(confirmed.phase == .ringing)
    }

    @Test func notFoundOnAttachIsUnanswered() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        #expect(call.handle(.bridgeEnded(.notFound)) == [.closeMedia, .reportEnded(.unanswered)])
    }

    @Test func bridgeUpdatesCallerName() {
        var (call, _) = CallSession.incoming(push: push(name: nil), now: start)
        let effects = call.handle(.bridgeConfirmedIncoming(IncomingCall(callId: callId, caller: "+4930123456", callerName: "Oma", startedAt: start)))
        #expect(effects == [.updateRemoteParty(number: "+4930123456", name: "Oma")])
        #expect(call.remoteName == "Oma")
    }

    @Test func announcedCallStartsRingingAndAttaches() {
        let (call, effects) = CallSession.incoming(
            announced: IncomingCall(callId: callId, caller: "0301", startedAt: start), now: start
        )
        #expect(call.phase == .ringing)
        #expect(effects == [.sendAttach])
    }

    @Test func hangupAfterAnswerIsHangupNotDecline() {
        var (call, _) = CallSession.incoming(push: push(), now: start)
        _ = call.handle(.offer(offer()))
        _ = call.handle(.localAnswerSent)
        _ = call.handle(.userAnswered)
        _ = call.handle(.remoteState(.connected), now: start + 1)
        #expect(call.handle(.userEnded, now: start + 11) == [.sendHangup(.hangup), .closeMedia])
        #expect(call.outcome == .answered)
        #expect(call.duration == 10)
    }

    // MARK: Outgoing

    @Test func outgoingWithLocalRingback() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        #expect(call.handle(.offer(offer())) == [.negotiate(offer())])
        #expect(call.handle(.localAnswerSent) == [])
        #expect(call.handle(.remoteState(.ringing)) == [.startRingback])
        #expect(call.handle(.remoteState(.connected), now: start + 5) == [.stopRingback, .reportOutgoingConnected])
        #expect(call.handle(.userEnded, now: start + 65) == [.sendHangup(.hangup), .closeMedia])
        #expect(call.outcome == .answered)
        #expect(call.duration == 60)
    }

    @Test func earlyMediaStopsAndSuppressesRingback() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        #expect(call.handle(.remoteState(.ringing)) == [.startRingback])
        #expect(call.handle(.remoteState(.earlyMedia)) == [.stopRingback])
        #expect(call.phase == .earlyMedia)
        #expect(call.handle(.remoteState(.connected)) == [.reportOutgoingConnected])
    }

    @Test func earlyMediaFirstNeverStartsRingback() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        #expect(call.handle(.remoteState(.earlyMedia)) == [])
        #expect(call.handle(.remoteState(.ringing)) == [])
    }

    @Test func busy() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        _ = call.handle(.remoteState(.ringing))
        #expect(call.handle(.bridgeEnded(.busy)) == [.stopRingback, .closeMedia, .reportEnded(.failed)])
        #expect(call.outcome == .busy)
    }

    @Test func cancelBeforeAnswer() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        _ = call.handle(.remoteState(.ringing))
        #expect(call.handle(.userEnded) == [.stopRingback, .sendHangup(.hangup), .closeMedia])
        #expect(call.outcome == .cancelled)
    }

    @Test func outgoingWithoutBridgeContactTimesOut() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        #expect(call.handle(.attachTimedOut) == [.sendHangup(.failed), .closeMedia, .reportEnded(.failed)])
        #expect(call.outcome == .failed)
    }

    @Test func reconnectReattaches() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        _ = call.handle(.remoteState(.connected))
        #expect(call.handle(.signalingReconnected) == [.sendAttach])
        // ICE restart: a second offer is negotiated again.
        #expect(call.handle(.offer(offer())) == [.negotiate(offer())])
    }

    @Test func endedCallIgnoresEverything() {
        var call = CallSession.outgoing(id: callId, number: "030123", name: nil, now: start)
        _ = call.handle(.bridgeEnded(.failed))
        #expect(call.handle(.remoteState(.connected)) == [])
        #expect(call.handle(.userEnded) == [])
        #expect(call.handle(.bridgeEnded(.remoteHangup)) == [])
    }

    // MARK: Mapping

    @Test func callKitReasonsFollowSpecTable() {
        #expect(CallEndReason.remoteHangup.callKitReason(for: .outgoing) == .remoteEnded)
        #expect(CallEndReason.remoteCancelled.callKitReason(for: .incoming) == .unanswered)
        #expect(CallEndReason.answeredElsewhere.callKitReason(for: .incoming) == .answeredElsewhere)
        #expect(CallEndReason.declinedElsewhere.callKitReason(for: .incoming) == .declinedElsewhere)
        #expect(CallEndReason.busy.callKitReason(for: .outgoing) == .failed)
        #expect(CallEndReason.rejected.callKitReason(for: .outgoing) == .failed)
        #expect(CallEndReason.failed.callKitReason(for: .incoming) == .failed)
        #expect(CallEndReason.notFound.callKitReason(for: .incoming) == .unanswered)
        #expect(CallEndReason.notFound.callKitReason(for: .outgoing) == .failed)
        #expect(CallEndReason.localHangup.callKitReason(for: .incoming) == nil)
    }
}
