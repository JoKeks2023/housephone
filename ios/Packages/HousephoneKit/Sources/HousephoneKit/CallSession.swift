import Foundation

public enum CallDirection: String, Codable, Sendable {
    case incoming
    case outgoing
}

/// The subset of `CXCallEndedReason` the protocol can produce. Kept here so
/// the mapping is testable without CallKit.
public enum CallKitEndReason: Sendable, Equatable {
    case failed
    case remoteEnded
    case unanswered
    case answeredElsewhere
    case declinedElsewhere
}

extension CallEndReason {
    /// Maps a `call.ended` reason to what CallKit should show, following the
    /// table in `docs/protocol/signaling-v1.md`. `nil` means the call already
    /// ended locally and must not be reported again.
    public func callKitReason(for direction: CallDirection) -> CallKitEndReason? {
        switch self {
        case .remoteHangup: .remoteEnded
        case .remoteCancelled: .unanswered
        case .answeredElsewhere: .answeredElsewhere
        case .declinedElsewhere: .declinedElsewhere
        case .busy, .rejected, .failed, .other: .failed
        case .notFound: direction == .incoming ? .unanswered : .failed
        case .localHangup: nil
        }
    }
}

public enum CallPhase: Sendable, Equatable {
    /// Incoming: CallKit rings, but the bridge hasn't confirmed the call yet.
    /// Outgoing: `call.dial` sent, nothing heard back.
    case waitingForBridge
    /// Incoming: the bridge confirmed the call, it rings here.
    /// Outgoing: the remote party rings.
    case ringing
    /// Outgoing: the remote side sends audio before answering.
    case earlyMedia
    /// Incoming: the user answered; waiting for the bridge to connect.
    case answering
    case connected
    case ended
}

/// How a call looks in the recents list.
public enum CallOutcome: String, Codable, Sendable {
    case answered
    case missed
    case declined
    case cancelled
    case busy
    case failed
    case answeredElsewhere
}

public enum CallEvent: Sendable, Equatable {
    /// `call.incoming` from the bridge.
    case bridgeConfirmedIncoming(IncomingCall)
    /// `call.offer` from the bridge (first offer or ICE restart).
    case offer(SessionOffer)
    /// The media engine created an answer and it went out as `call.answer`.
    case localAnswerSent
    /// `call.state` from the bridge.
    case remoteState(RemoteCallState)
    /// `call.ended` from the bridge.
    case bridgeEnded(CallEndReason)
    /// CallKit `CXAnswerCallAction`.
    case userAnswered
    /// CallKit `CXEndCallAction`.
    case userEnded
    /// No contact with the bridge within 10 s after the push.
    case attachTimedOut
    /// Signaling came back after a disconnect.
    case signalingReconnected
}

public enum CallEffect: Sendable, Equatable {
    case sendAttach
    case sendAccept
    case sendHangup(HangupReason)
    /// Hand the offer to the media engine, which answers with `call.answer`.
    case negotiate(SessionOffer)
    case updateRemoteParty(number: String, name: String?)
    case reportOutgoingConnected
    case reportEnded(CallKitEndReason)
    case startRingback
    case stopRingback
    case closeMedia
}

/// Pure state of one call as seen by this device. The app feeds it events
/// from CallKit, the bridge and the media engine, and executes the effects.
public struct CallSession: Sendable, Equatable, Identifiable {
    public let id: CallID
    public let direction: CallDirection
    public private(set) var remoteNumber: String
    public private(set) var remoteName: String?
    public private(set) var phase: CallPhase
    public let createdAt: Date
    public private(set) var connectedAt: Date?
    public private(set) var endedAt: Date?
    public private(set) var endReason: CallEndReason?
    public private(set) var outcome: CallOutcome?

    public private(set) var userAnswered = false
    public private(set) var answerSent = false
    public private(set) var acceptSent = false
    public private(set) var hasEarlyMedia = false
    public private(set) var isRingbackPlaying = false
    public private(set) var endedLocally = false

    private init(id: CallID, direction: CallDirection, number: String, name: String?, phase: CallPhase, now: Date) {
        self.id = id
        self.direction = direction
        self.remoteNumber = number
        self.remoteName = name
        self.phase = phase
        self.createdAt = now
    }

    /// An incoming call announced by a VoIP push. The app has already
    /// reported it to CallKit; it now connects and attaches.
    public static func incoming(push: IncomingCallPush, now: Date) -> (CallSession, [CallEffect]) {
        let session = CallSession(id: push.callId, direction: .incoming, number: push.caller, name: push.callerName, phase: .waitingForBridge, now: now)
        return (session, [.sendAttach])
    }

    /// An incoming call the bridge announced over an open connection. The
    /// app attaches as well, so the bridge sends the offer either way.
    public static func incoming(announced call: IncomingCall, now: Date) -> (CallSession, [CallEffect]) {
        let session = CallSession(id: call.callId, direction: .incoming, number: call.caller, name: call.callerName, phase: .ringing, now: now)
        return (session, [.sendAttach])
    }

    /// An outgoing call. The app has sent `call.dial` and reported
    /// "started connecting" to CallKit.
    public static func outgoing(id: CallID, number: String, name: String?, now: Date) -> CallSession {
        CallSession(id: id, direction: .outgoing, number: number, name: name, phase: .waitingForBridge, now: now)
    }

    public var isActive: Bool { phase != .ended }

    public var duration: TimeInterval? {
        guard let connectedAt else { return nil }
        return (endedAt ?? Date()).timeIntervalSince(connectedAt)
    }

    @discardableResult
    public mutating func handle(_ event: CallEvent, now: Date = Date()) -> [CallEffect] {
        guard phase != .ended else { return [] }

        switch (direction, event) {
        case (.incoming, .bridgeConfirmedIncoming(let call)):
            var effects: [CallEffect] = []
            if call.caller != remoteNumber || call.callerName != remoteName {
                remoteNumber = call.caller
                remoteName = call.callerName ?? remoteName
                effects.append(.updateRemoteParty(number: remoteNumber, name: remoteName))
            }
            if phase == .waitingForBridge { phase = .ringing }
            return effects

        case (.outgoing, .bridgeConfirmedIncoming):
            return []

        case (_, .offer(let offer)):
            if direction == .incoming, phase == .waitingForBridge { phase = .ringing }
            return [.negotiate(offer)]

        case (_, .localAnswerSent):
            answerSent = true
            return acceptIfReady()

        case (.incoming, .userAnswered):
            userAnswered = true
            if phase != .connected { phase = .answering }
            return acceptIfReady()

        case (.outgoing, .userAnswered):
            return []

        case (.incoming, .remoteState(.connected)):
            guard phase != .connected else { return [] }
            phase = .connected
            connectedAt = now
            return []

        case (.incoming, .remoteState):
            return []

        case (.outgoing, .remoteState(.ringing)):
            guard phase == .waitingForBridge else { return [] }
            phase = .ringing
            guard !hasEarlyMedia else { return [] }
            isRingbackPlaying = true
            return [.startRingback]

        case (.outgoing, .remoteState(.earlyMedia)):
            guard phase == .waitingForBridge || phase == .ringing else { return [] }
            phase = .earlyMedia
            hasEarlyMedia = true
            return stopRingbackIfPlaying()

        case (.outgoing, .remoteState(.connected)):
            guard phase != .connected else { return [] }
            phase = .connected
            connectedAt = now
            return stopRingbackIfPlaying() + [.reportOutgoingConnected]

        case (_, .bridgeEnded(let reason)):
            let callKitReason = reason.callKitReason(for: direction)
            finish(reason: reason, now: now)
            var effects = stopRingbackIfPlaying() + [.closeMedia]
            if let callKitReason { effects.append(.reportEnded(callKitReason)) }
            return effects

        case (_, .userEnded):
            let wasAnswered = direction == .outgoing || userAnswered || phase == .connected
            endedLocally = true
            finish(reason: .localHangup, now: now)
            return stopRingbackIfPlaying() + [.sendHangup(wasAnswered ? .hangup : .declined), .closeMedia]

        case (.incoming, .attachTimedOut):
            guard phase == .waitingForBridge else { return [] }
            finish(reason: .failed, now: now)
            return [.closeMedia, .reportEnded(.failed)]

        case (.outgoing, .attachTimedOut):
            guard phase == .waitingForBridge else { return [] }
            finish(reason: .failed, now: now)
            return [.sendHangup(.failed), .closeMedia, .reportEnded(.failed)]

        case (_, .signalingReconnected):
            return [.sendAttach]
        }
    }

    private mutating func acceptIfReady() -> [CallEffect] {
        guard direction == .incoming, userAnswered, answerSent, !acceptSent else { return [] }
        acceptSent = true
        return [.sendAccept]
    }

    private mutating func stopRingbackIfPlaying() -> [CallEffect] {
        guard isRingbackPlaying else { return [] }
        isRingbackPlaying = false
        return [.stopRingback]
    }

    private mutating func finish(reason: CallEndReason, now: Date) {
        let wasConnected = connectedAt != nil
        phase = .ended
        endedAt = now
        endReason = reason
        outcome = Self.outcome(direction: direction, reason: reason, wasConnected: wasConnected, userAnswered: userAnswered)
    }

    static func outcome(direction: CallDirection, reason: CallEndReason, wasConnected: Bool, userAnswered: Bool) -> CallOutcome {
        if wasConnected { return .answered }
        switch (direction, reason) {
        case (_, .answeredElsewhere): return .answeredElsewhere
        case (.incoming, .localHangup): return userAnswered ? .failed : .declined
        case (.incoming, .declinedElsewhere): return .declined
        case (.incoming, .remoteCancelled), (.incoming, .notFound): return .missed
        case (.incoming, _): return userAnswered ? .failed : .missed
        case (.outgoing, .localHangup): return .cancelled
        case (.outgoing, .busy): return .busy
        case (.outgoing, _): return .failed
        }
    }
}
