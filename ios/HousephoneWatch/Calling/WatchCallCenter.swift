import AVFAudio
import CallKit
import HousephoneKit
import Observation
import os
import PushKit
import WatchKit

/// Something the user should know about when a call could not start.
enum WatchCallFailure: Identifiable, Equatable {
    case notPaired
    case invalidNumber
    case callInProgress
    case bridgeUnreachable
    case system(String)

    var id: String { String(describing: self) }

    var message: LocalizedStringResource {
        switch self {
        case .notPaired: "Koppel die Watch zuerst in der iPhone-App."
        case .invalidNumber: "Diese Nummer kann nicht gewählt werden."
        case .callInProgress: "Es läuft bereits ein Anruf."
        case .bridgeUnreachable: "Die Bridge ist nicht erreichbar."
        case .system(let description): "Der Anruf konnte nicht gestartet werden: \(description)"
        }
    }
}

/// Coordinates CallKit, PushKit, the bridge and the audio of the watch.
/// One call at a time.
///
/// Differences to the iPhone:
/// - WebSocket is only allowed while a CallKit call runs (TN3135), so each
///   call opens its own signaling connection and closes it at the end.
/// - Network access may only open up once CallKit activated the audio
///   session. A ringing call therefore does not fail when the bridge is not
///   reachable yet; it fails only if there is still no connection 10 s
///   after the user answered.
/// - While it rings without a WebSocket, the watch polls the call status
///   over HTTPS, so it stops ringing when the caller hangs up or someone
///   else answers.
/// - Audio is A-law over the WebSocket (`call.media`), not WebRTC.
@MainActor
@Observable
final class WatchCallCenter: NSObject {
    enum ConnectionState: Equatable {
        case connecting
        case connected
        case reconnecting
    }

    private(set) var activeCall: CallSession?
    private(set) var isMuted = false
    private(set) var connection: ConnectionState?
    var failure: WatchCallFailure?

    /// Name for a number the push or the dialer did not name, e.g. from
    /// the FRITZ!Box phonebook.
    @ObservationIgnored var nameLookup: ((String) -> String?)?

    @ObservationIgnored private let provider: CXProvider
    @ObservationIgnored private let callController = CXCallController()
    @ObservationIgnored private let pushRegistry = PKPushRegistry(queue: .main)
    @ObservationIgnored private let bridge: WatchBridge
    @ObservationIgnored private let recents: RecentCalls
    @ObservationIgnored let audio = CallAudio()
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone.watch", category: "calls")

    @ObservationIgnored private var client: SignalingClient?
    @ObservationIgnored private var clientTasks: [Task<Void, Never>] = []
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var attachedGeneration: Int?
    @ObservationIgnored private var dialSent = false
    @ObservationIgnored private var lastSend: Task<Void, Never>?
    @ObservationIgnored private var outgoingAudio: AsyncStream<Data>.Continuation?
    @ObservationIgnored private var deadline: Task<Void, Never>?
    @ObservationIgnored private var ringingTimeout: Task<Void, Never>?
    @ObservationIgnored private var statusPoll: Task<Void, Never>?
    @ObservationIgnored private var clearTask: Task<Void, Never>?
    @ObservationIgnored private var pendingOutgoingNames: [UUID: String] = [:]
    @ObservationIgnored private var callsNotRecorded: Set<CallID> = []

    /// Time to reach the bridge once the user answered or dialed.
    static let connectTimeout: Duration = .seconds(10)
    /// A call nobody could confirm stops ringing after this.
    static let unconfirmedRingingTimeout: Duration = .seconds(60)
    /// A confirmed call normally ends via CANCEL; this is the safety net
    /// should the connection drop while it rings.
    static let confirmedRingingTimeout: Duration = .seconds(180)
    /// How often a ringing call asks the bridge whether it is still ringing.
    static let statusPollInterval: Duration = .seconds(2)

    init(bridge: WatchBridge, recents: RecentCalls) {
        self.bridge = bridge
        self.recents = recents
        let configuration = CXProviderConfiguration()
        configuration.maximumCallGroups = 1
        configuration.maximumCallsPerCallGroup = 1
        configuration.supportedHandleTypes = [.phoneNumber, .generic]
        configuration.includesCallsInRecents = true
        provider = CXProvider(configuration: configuration)
        super.init()

        provider.setDelegate(self, queue: nil)
        pushRegistry.delegate = self
        updatePushRegistration(paired: bridge.isPaired)
        bridge.onPairedChange = { [weak self] paired in self?.updatePushRegistration(paired: paired) }
    }

    /// Only a paired watch asks for VoIP pushes. Unpairing drops the token,
    /// so APNs answers the bridge with 410 should it still try.
    private func updatePushRegistration(paired: Bool) {
        pushRegistry.desiredPushTypes = paired ? [.voIP] : []
    }

    var hasActiveCall: Bool { activeCall?.isActive == true }

    // MARK: - User actions

    @discardableResult
    func startCall(to rawNumber: String, name: String? = nil) async -> Bool {
        guard let number = PhoneNumber.dialable(rawNumber) else {
            failure = .invalidNumber
            return false
        }
        guard bridge.isPaired else {
            failure = .notPaired
            return false
        }
        guard !hasActiveCall else {
            failure = .callInProgress
            return false
        }
        let uuid = UUID()
        pendingOutgoingNames[uuid] = name
        let action = CXStartCallAction(call: uuid, handle: CXHandle(type: .phoneNumber, value: number))
        do {
            try await callController.request(CXTransaction(action: action))
            return true
        } catch {
            pendingOutgoingNames[uuid] = nil
            failure = .system(error.localizedDescription)
            return false
        }
    }

    func answer() {
        guard let call = activeCall, call.direction == .incoming, call.isActive else { return }
        request(CXAnswerCallAction(call: call.id.uuid))
    }

    func end() {
        guard let call = activeCall, call.isActive else { return }
        request(CXEndCallAction(call: call.id.uuid))
    }

    func setMuted(_ muted: Bool) {
        guard let call = activeCall, call.isActive else { return }
        request(CXSetMutedCallAction(call: call.id.uuid, muted: muted))
    }

    func playDTMF(_ digits: String) {
        guard let call = activeCall, call.phase == .connected || call.phase == .earlyMedia, PhoneNumber.isDTMF(digits) else { return }
        request(CXPlayDTMFCallAction(call: call.id.uuid, digits: digits, type: .singleTone))
    }

    private func request(_ action: CXCallAction) {
        callController.request(CXTransaction(action: action)) { [logger] error in
            if let error { logger.error("CallKit request failed: \(error.localizedDescription, privacy: .public)") }
        }
    }

    // MARK: - Signaling connection (one per call)

    private func openConnection() {
        guard client == nil else {
            Task { await client?.refreshConnection() }
            return
        }
        guard let client = bridge.makeCallClient() else { return }
        self.client = client
        connection = .connecting

        let (frames, continuation) = AsyncStream.makeStream(of: Data.self, bufferingPolicy: .bufferingNewest(25))
        outgoingAudio = continuation
        audio.onFrame = { frame in continuation.yield(frame) }

        clientTasks = [
            Task { [weak self] in
                for await event in client.events {
                    // Late events of a closed connection must not touch
                    // the next call.
                    guard let self, self.client === client else { continue }
                    self.handle(event)
                }
            },
            // Audio stays off the main actor.
            Task.detached { [audio] in
                for await message in client.audio {
                    audio.receive(message)
                }
            },
            Task.detached {
                for await frame in frames {
                    try? await client.sendAudio(frame)
                }
            },
        ]
        Task { await client.start() }
    }

    private func closeConnection(after delay: Duration = .zero) {
        guard let client else { return }
        let tasks = clientTasks
        self.client = nil
        clientTasks = []
        outgoingAudio?.finish()
        outgoingAudio = nil
        audio.onFrame = nil
        connection = nil
        attachedGeneration = nil
        Task {
            // Let a final hangup leave before the socket closes.
            if delay > .zero { try? await Task.sleep(for: delay) }
            await client.stop()
            for task in tasks { task.cancel() }
        }
    }

    private func handle(_ event: SignalingClient.Event) {
        switch event {
        case .state(let state):
            switch state {
            case .connected:
                generation += 1
                connection = .connected
                deadline?.cancel()
                connectedToBridge()
            case .connecting:
                if connection != .connected { connection = .connecting }
            case .waitingToReconnect, .disconnected:
                if connection == .connected { connection = .reconnecting }
            case .unauthorized:
                bridge.markRejected()
                if let call = activeCall, call.isActive {
                    apply(.bridgeEnded(.failed), to: call.id)
                }
            case .untrustedBridge, .clockSkew:
                // No retry without the bridge check; the call can't go on.
                if let call = activeCall, call.isActive {
                    apply(.bridgeEnded(.failed), to: call.id)
                }
            }
        case .message(let message):
            handle(message)
        }
    }

    private func connectedToBridge() {
        guard let call = activeCall, call.isActive else { return }
        if call.direction == .outgoing, !dialSent {
            dialSent = true
            attachedGeneration = generation
            send(.callDial(DialRequest(callId: call.id, number: call.remoteNumber)))
        } else if attachedGeneration != generation {
            attachedGeneration = generation
            audio.resetPlayout()
            send(.callAttach(CallReference(callId: call.id)))
        }
    }

    private func handle(_ message: SignalingMessage) {
        switch message {
        case .callIncoming(let incoming):
            apply(.bridgeConfirmedIncoming(incoming), to: incoming.callId)
            startRingingTimeout(for: incoming.callId, after: Self.confirmedRingingTimeout)
        case .callMedia(let media):
            apply(.webSocketMedia(media), to: media.callId)
        case .callOffer(let offer):
            // This device announced websocket-pcma only; a WebRTC offer
            // means an incompatible bridge.
            logger.error("Unexpected WebRTC offer; this bridge does not support the watch")
            send(.callHangup(Hangup(callId: offer.callId, reason: .failed)))
            apply(.bridgeEnded(.failed), to: offer.callId)
        case .callState(let change):
            apply(.remoteState(change.state), to: change.callId)
        case .callEnded(let ended):
            apply(.bridgeEnded(ended.reason), to: ended.callId)
        case .error(let error):
            logger.error("Bridge error \(error.code.rawValue, privacy: .public): \(error.message, privacy: .public)")
            if error.callId != nil {
                failure = error.code == .invalidNumber ? .invalidNumber : .bridgeUnreachable
            }
        default:
            break
        }
    }

    // MARK: - State machine plumbing

    private func begin(_ session: CallSession) {
        clearTask?.cancel()
        activeCall = session
        isMuted = false
        dialSent = false
        attachedGeneration = nil
        audio.setMuted(false)
        audio.resetPlayout()
    }

    private func apply(_ event: CallEvent, to callId: CallID) {
        guard var call = activeCall, call.id == callId else { return }
        let effects = call.handle(event, now: .now)
        activeCall = call
        perform(effects, for: callId)
        if call.phase == .ended, effects.contains(.closeMedia) {
            finish(call)
        }
    }

    private func perform(_ effects: [CallEffect], for callId: CallID) {
        for effect in effects {
            switch effect {
            case .sendAttach:
                openConnection()
                if connection == .connected { connectedToBridge() }
            case .sendAccept:
                send(.callAccept(CallReference(callId: callId)))
            case .sendHangup(let reason):
                send(.callHangup(Hangup(callId: callId, reason: reason)))
            case .negotiate:
                break
            case .startWebSocketMedia(let media):
                if !media.isSupported {
                    logger.error("Unsupported media format \(media.codec, privacy: .public)")
                    send(.callHangup(Hangup(callId: callId, reason: .failed)))
                    apply(.bridgeEnded(.failed), to: callId)
                }
            case .updateRemoteParty(let number, let name):
                provider.reportCall(with: callId.uuid, updated: callUpdate(number: number, name: name))
            case .reportOutgoingConnected:
                provider.reportOutgoingCall(with: callId.uuid, connectedAt: nil)
            case .reportEnded(let reason):
                provider.reportCall(with: callId.uuid, endedAt: nil, reason: reason.cxReason)
            case .startRingback:
                audio.setRingback(true)
            case .stopRingback:
                audio.setRingback(false)
            case .closeMedia:
                audio.setRingback(false)
            }
        }
    }

    private func finish(_ call: CallSession) {
        deadline?.cancel()
        ringingTimeout?.cancel()
        stopStatusPolling()
        closeConnection(after: .milliseconds(500))
        isMuted = false
        if callsNotRecorded.remove(call.id) == nil, let record = RecentCall(session: call) {
            recents.add(record)
        }
        clearTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled, let self, self.activeCall?.id == call.id else { return }
            self.activeCall = nil
        }
    }

    /// Fails the call if the bridge is still unreachable after `connectTimeout`.
    private func startConnectDeadline(for callId: CallID) {
        deadline?.cancel()
        guard connection != .connected else { return }
        deadline = Task { [weak self] in
            try? await Task.sleep(for: Self.connectTimeout)
            guard !Task.isCancelled, let self, self.connection != .connected,
                  self.activeCall?.id == callId, self.activeCall?.isActive == true
            else { return }
            self.logger.error("Bridge unreachable; ending call")
            self.failure = .bridgeUnreachable
            self.apply(.bridgeEnded(.failed), to: callId)
        }
    }

    private func startRingingTimeout(for callId: CallID, after timeout: Duration) {
        ringingTimeout?.cancel()
        guard activeCall?.id == callId, activeCall?.userAnswered == false else { return }
        ringingTimeout = Task { [weak self] in
            try? await Task.sleep(for: timeout)
            guard !Task.isCancelled, let self, let call = self.activeCall, call.id == callId,
                  call.isActive, !call.userAnswered
            else { return }
            self.apply(.bridgeEnded(.remoteCancelled), to: callId)
        }
    }

    // MARK: - Call status while ringing

    /// Asks the bridge every 2 s whether the call still rings, as long as
    /// it rings here and has no WebSocket (watchOS may not allow one before
    /// the call is answered). Network errors are ignored; the ringing
    /// timeouts stay the safety net.
    private func startStatusPolling(for callId: CallID) {
        statusPoll?.cancel()
        statusPoll = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: Self.statusPollInterval)
                guard !Task.isCancelled, let self else { return }
                guard let call = self.activeCall, call.id == callId, call.isActive, !call.userAnswered else { return }
                if self.connection == .connected, self.attachedGeneration != nil {
                    // Attached: call.ended arrives over the WebSocket.
                    continue
                }
                let status = await self.bridge.callStatus(callId)
                guard !Task.isCancelled else { return }
                if let status {
                    self.applyPolledStatus(status, to: callId)
                } else if self.bridge.isRejected {
                    self.apply(.bridgeEnded(.failed), to: callId)
                }
            }
        }
    }

    private func stopStatusPolling() {
        statusPoll?.cancel()
        statusPoll = nil
    }

    private func applyPolledStatus(_ status: BridgeCallStatus, to callId: CallID) {
        guard let call = activeCall, call.id == callId, call.isActive, !call.userAnswered else { return }
        switch status.state {
        case .ended(let reason, _):
            logger.info("Call ended while ringing: \(reason.rawValue, privacy: .public)")
            apply(.bridgeEnded(reason), to: callId)
        case .ringing, .connected:
            break
        }
    }

    // MARK: - Signaling

    /// Sends in order: each message waits for the previous one.
    private func send(_ message: SignalingMessage) {
        let previous = lastSend
        let client = client
        lastSend = Task { [logger] in
            await previous?.value
            guard let client else { return }
            do {
                try await client.send(message)
            } catch {
                logger.error("Sending \(message.type, privacy: .public) failed: \(String(describing: error), privacy: .public)")
            }
        }
    }

    // MARK: - CallKit helpers

    private func callUpdate(number: String, name: String?) -> CXCallUpdate {
        let update = CXCallUpdate()
        let trimmed = number.trimmingCharacters(in: .whitespaces)
        update.remoteHandle = trimmed.isEmpty
            ? CXHandle(type: .generic, value: String(localized: "Unbekannt"))
            : CXHandle(type: .phoneNumber, value: trimmed)
        let given = name?.trimmingCharacters(in: .whitespaces)
        update.localizedCallerName = given?.isEmpty == false ? given : (trimmed.isEmpty ? nil : nameLookup?(trimmed))
        update.hasVideo = false
        update.supportsDTMF = true
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        return update
    }

    private func reportIncoming(_ session: CallSession, completion: @escaping @Sendable () -> Void) {
        let callId = session.id
        provider.reportNewIncomingCall(with: callId.uuid, update: callUpdate(number: session.remoteNumber, name: session.remoteName)) { [weak self] error in
            Task { @MainActor in
                defer { completion() }
                guard let error, let self else { return }
                if let callKitError = error as? CXErrorCodeIncomingCallError, callKitError.code == .callUUIDAlreadyExists { return }
                self.logger.info("CallKit refused incoming call: \(error.localizedDescription, privacy: .public)")
                guard self.activeCall?.id == callId else { return }
                self.callsNotRecorded.insert(callId)
                self.apply(.userEnded, to: callId)
            }
        }
    }

    private func reportAndEndImmediately(_ uuid: UUID, update: CXCallUpdate, completion: @escaping @Sendable () -> Void) {
        provider.reportNewIncomingCall(with: uuid, update: update) { [weak self] _ in
            Task { @MainActor in
                self?.provider.reportCall(with: uuid, endedAt: nil, reason: .failed)
                completion()
            }
        }
    }
}

// MARK: - PushKit

extension WatchCallCenter: @preconcurrency PKPushRegistryDelegate {
    func pushRegistry(_ registry: PKPushRegistry, didUpdate pushCredentials: PKPushCredentials, for type: PKPushType) {
        guard type == .voIP else { return }
        bridge.updatePushToken(pushCredentials.token.map { String(format: "%02x", $0) }.joined())
    }

    func pushRegistry(_ registry: PKPushRegistry, didInvalidatePushTokenFor type: PKPushType) {
        guard type == .voIP else { return }
        bridge.updatePushToken(nil)
    }

    func pushRegistry(_ registry: PKPushRegistry, didReceiveIncomingPushWith payload: PKPushPayload, for type: PKPushType, completion: @escaping () -> Void) {
        let done = UncheckedCompletion(completion)
        guard type == .voIP else {
            completion()
            return
        }

        // Every VoIP push must become a CallKit call, or the system stops
        // delivering them.
        guard let push = try? IncomingCallPush(dictionary: payload.dictionaryPayload) else {
            let update = CXCallUpdate()
            update.remoteHandle = CXHandle(type: .generic, value: String(localized: "Unbekannt"))
            reportAndEndImmediately(UUID(), update: update) { done.call() }
            return
        }
        let update = callUpdate(number: push.caller, name: push.callerName)

        guard bridge.isPaired else {
            reportAndEndImmediately(push.callId.uuid, update: update) { done.call() }
            return
        }
        if let call = activeCall, call.isActive {
            // Busy, or a repeated push for the current call: CallKit rejects
            // the duplicate or second call, which is what we want.
            provider.reportNewIncomingCall(with: push.callId.uuid, update: update) { _ in done.call() }
            return
        }

        let (session, effects) = CallSession.incoming(push: push, now: .now)
        begin(session)
        reportIncoming(session) { done.call() }
        startRingingTimeout(for: session.id, after: Self.unconfirmedRingingTimeout)
        startStatusPolling(for: session.id)
        perform(effects, for: session.id)
    }
}

/// A PushKit completion handler, called exactly once on the main actor.
struct UncheckedCompletion: @unchecked Sendable {
    private let completion: () -> Void

    init(_ completion: @escaping () -> Void) {
        self.completion = completion
    }

    func call() {
        completion()
    }
}

// MARK: - CallKit

extension WatchCallCenter: @preconcurrency CXProviderDelegate {
    func providerDidReset(_ provider: CXProvider) {
        logger.info("Provider reset")
        stopStatusPolling()
        audio.stop()
        if let call = activeCall, call.isActive {
            send(.callHangup(Hangup(callId: call.id, reason: .failed)))
        }
        closeConnection(after: .milliseconds(500))
        activeCall = nil
        isMuted = false
    }

    func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        guard !hasActiveCall else {
            action.fail()
            return
        }
        let callId = CallID(action.callUUID)
        let number = action.handle.value
        let name = pendingOutgoingNames.removeValue(forKey: action.callUUID)

        CallAudio.configureSession()
        begin(CallSession.outgoing(id: callId, number: number, name: name, now: .now))
        action.fulfill()
        provider.reportOutgoingCall(with: action.callUUID, startedConnectingAt: nil)
        openConnection()
        startConnectDeadline(for: callId)
    }

    func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) {
        guard let call = activeCall, call.id.uuid == action.callUUID, call.isActive else {
            action.fail()
            return
        }
        CallAudio.configureSession()
        ringingTimeout?.cancel()
        stopStatusPolling()
        apply(.userAnswered, to: call.id)
        openConnection()
        startConnectDeadline(for: call.id)
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        if let call = activeCall, call.id.uuid == action.callUUID {
            apply(.userEnded, to: call.id)
        }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        audio.setMuted(action.isMuted)
        isMuted = action.isMuted
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXPlayDTMFCallAction) {
        guard let call = activeCall, call.id.uuid == action.callUUID, PhoneNumber.isDTMF(action.digits) else {
            action.fail()
            return
        }
        send(.callDTMF(DTMFDigits(callId: call.id, digits: action.digits)))
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXSetHeldCallAction) {
        action.fail()
    }

    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        audio.start()
        // Network access may only have opened up now.
        if connection != .connected { Task { await client?.refreshConnection() } }
    }

    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        audio.stop()
    }

    func provider(_ provider: CXProvider, timedOutPerforming action: CXAction) {
        logger.error("CallKit action timed out: \(String(describing: type(of: action)), privacy: .public)")
    }
}

extension CallKitEndReason {
    var cxReason: CXCallEndedReason {
        switch self {
        case .failed: .failed
        case .remoteEnded: .remoteEnded
        case .unanswered: .unanswered
        case .answeredElsewhere: .answeredElsewhere
        case .declinedElsewhere: .declinedElsewhere
        }
    }
}
