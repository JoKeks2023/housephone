import AppKit
import Foundation
import HousephoneKit
import Observation
import os
import UserNotifications

/// Something the user should know about when a call could not start.
enum MacCallFailure: Identifiable, Equatable {
    case notPaired
    case bridgeOffline
    case invalidNumber
    case callInProgress

    var id: String { message }

    var message: String {
        switch self {
        case .notPaired: "Kopple zuerst deine Bridge, dann kannst du telefonieren."
        case .bridgeOffline: "Die Bridge ist gerade nicht erreichbar. Prüfe die Internetverbindung und versuche es erneut."
        case .invalidNumber: "Diese Nummer kann nicht gewählt werden."
        case .callInProgress: "Es läuft bereits ein Anruf."
        }
    }
}

/// Calls on the Mac. There is no CallKit or PushKit here: the call state
/// machine (`CallSession`) is driven directly, audio runs as G.711 over the
/// bridge's WebSocket (`CallAudio`, signaling v1.4), and incoming calls
/// arrive only while the app runs (ADR in T-0018).
@MainActor
@Observable
final class MacCallCenter {
    private(set) var activeCall: CallSession?
    private(set) var isMuted = false
    var failure: MacCallFailure?

    @ObservationIgnored private let bridge: MacBridge
    @ObservationIgnored private let audio = CallAudio(logSubsystem: "com.jorisconrad.housephone.mac")
    @ObservationIgnored private var audioCall: CallID?
    @ObservationIgnored private var audioRunning = false
    @ObservationIgnored private var attachedGeneration: [CallID: Int] = [:]
    @ObservationIgnored private var lastSend: Task<Void, Never>?
    @ObservationIgnored private var clearTask: Task<Void, Never>?
    @ObservationIgnored private var ringTask: Task<Void, Never>?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone.mac", category: "calls")

    /// How long the bridge may take to confirm a call before it fails.
    static let attachTimeout: Duration = .seconds(10)

    init(bridge: MacBridge) {
        self.bridge = bridge
        bridge.onMessage = { [weak self] message in self?.handle(message) }
        bridge.onConnected = { [weak self] in self?.bridgeDidConnect() }
    }

    var hasActiveCall: Bool { activeCall?.isActive == true }

    // MARK: - User actions

    @discardableResult
    func startCall(to rawNumber: String, name: String? = nil) -> Bool {
        guard let number = PhoneNumber.dialable(rawNumber) else {
            failure = .invalidNumber
            return false
        }
        guard !hasActiveCall else {
            failure = .callInProgress
            return false
        }
        guard bridge.isPaired, bridge.status != .rejected else {
            failure = .notPaired
            return false
        }
        bridge.refresh()
        let session = CallSession.outgoing(id: CallID(), number: number, name: name, now: .now)
        begin(session)
        scheduleAttachTimeout(for: session.id)
        dial(session.id, number: number)
        return true
    }

    func answer() {
        guard let call = activeCall, call.direction == .incoming, call.isActive else { return }
        stopRinging()
        apply(.userAnswered, to: call.id)
    }

    /// Ends a running call, or declines a ringing one.
    func end() {
        guard let call = activeCall, call.isActive else { return }
        apply(.userEnded, to: call.id)
    }

    func setMuted(_ muted: Bool) {
        guard hasActiveCall else { return }
        isMuted = muted
        audio.setMuted(muted)
    }

    // MARK: - Bridge messages

    private func handle(_ message: SignalingMessage) {
        switch message {
        case .callIncoming(let incoming):
            if activeCall?.id == incoming.callId {
                apply(.bridgeConfirmedIncoming(incoming), to: incoming.callId)
            } else if !hasActiveCall {
                let (session, effects) = CallSession.incoming(announced: incoming, now: .now)
                begin(session)
                perform(effects, for: session.id)
                startRinging(for: session)
            } else {
                // One call at a time: the bridge keeps ringing the other devices.
                logger.info("Ignoring a second incoming call")
            }
        case .callMedia(let callMedia):
            apply(.webSocketMedia(callMedia), to: callMedia.callId)
        case .callState(let change):
            apply(.remoteState(change.state), to: change.callId)
        case .callEnded(let ended):
            apply(.bridgeEnded(ended.reason), to: ended.callId)
        case .callOffer:
            // This Mac announces only `websocket-pcma`; a WebRTC offer is a bridge bug.
            logger.error("Unexpected WebRTC offer; the Mac has no WebRTC")
        case .error(let error):
            logger.error("Bridge error \(error.code.rawValue, privacy: .public): \(error.message, privacy: .public)")
            if error.callId != nil, error.code == .sipUnavailable {
                failure = .bridgeOffline
            } else if error.callId != nil, error.code == .invalidNumber {
                failure = .invalidNumber
            }
        default:
            break
        }
    }

    private func bridgeDidConnect() {
        guard let call = activeCall, call.isActive,
              let generation = attachedGeneration[call.id], generation != bridge.generation
        else { return }
        apply(.signalingReconnected, to: call.id)
    }

    // MARK: - State machine plumbing

    private func begin(_ session: CallSession) {
        clearTask?.cancel()
        activeCall = session
        isMuted = false
        audio.setMuted(false)
    }

    private func apply(_ event: CallEvent, to callId: CallID) {
        guard var call = activeCall, call.id == callId else { return }
        let effects = call.handle(event, now: .now)
        activeCall = call
        perform(effects, for: callId)
        updateAudioRunning()
        if call.phase == .ended, effects.contains(.closeMedia) {
            finish(call)
        }
    }

    private func perform(_ effects: [CallEffect], for callId: CallID) {
        for effect in effects {
            switch effect {
            case .sendAttach:
                attach(callId)
            case .sendAccept:
                send(.callAccept(CallReference(callId: callId)))
            case .sendHangup(let reason):
                send(.callHangup(Hangup(callId: callId, reason: reason)))
            case .negotiate:
                break
            case .startWebSocketMedia(let callMedia):
                startAudio(callMedia, for: callId)
            case .updateRemoteParty, .reportOutgoingConnected, .reportEnded:
                break
            case .startRingback:
                if audioCall == callId { audio.setRingback(true) }
            case .stopRingback:
                audio.setRingback(false)
            case .closeMedia:
                stopAudio(callId)
            }
        }
    }

    private func finish(_ call: CallSession) {
        stopRinging()
        isMuted = false
        attachedGeneration[call.id] = nil
        // Keep the ended call on screen briefly so the user sees the outcome.
        clearTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled, let self, self.activeCall?.id == call.id else { return }
            self.activeCall = nil
        }
    }

    private func scheduleAttachTimeout(for callId: CallID) {
        Task { [weak self] in
            try? await Task.sleep(for: Self.attachTimeout)
            self?.apply(.attachTimedOut, to: callId)
        }
    }

    // MARK: - Signaling

    /// Sends in order: each message waits for the previous one.
    @discardableResult
    private func send(_ message: SignalingMessage) -> Task<Void, Never> {
        let previous = lastSend
        let task = Task { [bridge, logger] in
            await previous?.value
            do {
                try await bridge.send(message)
            } catch {
                logger.error("Sending \(message.type, privacy: .public) failed: \(String(describing: error), privacy: .public)")
            }
        }
        lastSend = task
        return task
    }

    private func attach(_ callId: CallID) {
        Task {
            do {
                _ = try await bridge.ensureConnected(timeout: Self.attachTimeout)
            } catch {
                logger.error("No bridge connection for attach: \(String(describing: error), privacy: .public)")
                return
            }
            guard activeCall?.id == callId, activeCall?.isActive == true,
                  attachedGeneration[callId] != bridge.generation
            else { return }
            attachedGeneration[callId] = bridge.generation
            send(.callAttach(CallReference(callId: callId)))
        }
    }

    private func dial(_ callId: CallID, number: String) {
        Task {
            do {
                _ = try await bridge.ensureConnected(timeout: Self.attachTimeout)
            } catch {
                failure = .bridgeOffline
                apply(.attachTimedOut, to: callId)
                return
            }
            guard activeCall?.id == callId, activeCall?.isActive == true else { return }
            attachedGeneration[callId] = bridge.generation
            send(.callDial(DialRequest(callId: callId, number: number)))
        }
    }

    // MARK: - Audio over the WebSocket

    private func startAudio(_ callMedia: CallMedia, for callId: CallID) {
        guard callMedia.isSupported else {
            logger.error("Unsupported media format \(callMedia.codec, privacy: .public)")
            send(.callHangup(Hangup(callId: callId, reason: .failed)))
            apply(.bridgeEnded(.failed), to: callId)
            return
        }
        if audioCall == callId {
            // Re-attach on a new connection: drop stale audio.
            audio.resetPlayout()
            return
        }
        audioCall = callId
        audio.setMuted(isMuted)

        let (frames, output) = AsyncStream.makeStream(of: Data.self, bufferingPolicy: .bufferingNewest(25))
        audio.onFrame = { frame in output.yield(frame) }
        audioOutput = output
        bridge.audio.setHandler { [audio] message in audio.receive(message) }
        let client = bridge.audioClient
        Task.detached {
            for await frame in frames {
                try? await client?.sendAudio(frame)
            }
        }
        if activeCall?.isRingbackPlaying == true { audio.setRingback(true) }
        updateAudioRunning()
    }

    @ObservationIgnored private var audioOutput: AsyncStream<Data>.Continuation?

    /// The microphone opens only for a call the user placed or answered,
    /// never while an incoming call is still ringing.
    private func updateAudioRunning() {
        guard let call = activeCall, call.isActive, audioCall == call.id else { return }
        let shouldRun = call.direction == .outgoing || call.userAnswered || call.phase == .connected
        guard shouldRun, !audioRunning else { return }
        audioRunning = true
        audio.start()
    }

    private func stopAudio(_ callId: CallID) {
        guard audioCall == callId else { return }
        audioCall = nil
        audioRunning = false
        bridge.audio.setHandler(nil)
        audio.onFrame = nil
        audioOutput?.finish()
        audioOutput = nil
        audio.setRingback(false)
        audio.stop()
    }

    // MARK: - Incoming call alert

    private func startRinging(for session: CallSession) {
        NSApp.activate(ignoringOtherApps: true)
        let who = session.remoteName ?? (session.remoteNumber.isEmpty ? "Unbekannt" : session.remoteNumber)
        let content = UNMutableNotificationContent()
        content.title = "Eingehender Anruf"
        content.body = who
        content.interruptionLevel = .timeSensitive
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert]) { granted, _ in
            guard granted else { return }
            UNUserNotificationCenter.current().add(
                UNNotificationRequest(identifier: session.id.description, content: content, trigger: nil)
            )
        }
        ringTask?.cancel()
        ringTask = Task {
            while !Task.isCancelled {
                NSSound(named: NSSound.Name("Submarine"))?.play()
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    private func stopRinging() {
        ringTask?.cancel()
        ringTask = nil
        if let id = activeCall?.id {
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [id.description])
        }
    }
}
