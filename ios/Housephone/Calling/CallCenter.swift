import AVFAudio
import CallKit
import HousephoneKit
import Observation
import os
import PushKit
import SwiftData
import UIKit

/// Something the user should know about when a call could not start.
enum CallFailure: Identifiable, Equatable {
    case notPaired
    case bridgeOffline
    case invalidNumber
    case callInProgress
    case system(String)

    var id: String { message.key }

    var message: LocalizedStringResource {
        switch self {
        case .notPaired: "Koppel zuerst deine Bridge, dann kannst du telefonieren."
        case .bridgeOffline: "Die Bridge ist gerade nicht erreichbar. Prüfe die Internetverbindung und versuche es erneut."
        case .invalidNumber: "Diese Nummer kann nicht gewählt werden."
        case .callInProgress: "Es läuft bereits ein Anruf."
        case .system(let description): "Der Anruf konnte nicht gestartet werden: \(description)"
        }
    }
}

/// Coordinates CallKit, PushKit, the bridge, WebRTC and the recents list.
/// Supports one call at a time.
///
/// The rules that matter most:
/// - Every VoIP push is reported to CallKit synchronously, before anything
///   else happens (iOS terminates apps that don't).
/// - Audio only runs between CallKit's `didActivate` and `didDeactivate`.
/// - All call state transitions go through `CallSession` in HousephoneKit.
@MainActor
@Observable
final class CallCenter: NSObject {
    private(set) var activeCall: CallSession?
    private(set) var isMuted = false
    private(set) var mediaState: MediaEngine.ConnectionState?
    var failure: CallFailure?

    @ObservationIgnored private let provider: CXProvider
    @ObservationIgnored private let callController = CXCallController()
    @ObservationIgnored private let pushRegistry = PKPushRegistry(queue: .main)
    @ObservationIgnored private let bridge: BridgeConnection
    @ObservationIgnored private let contacts: ContactsDirectory
    @ObservationIgnored private let media = MediaEngine()
    @ObservationIgnored private let ringback = RingbackPlayer()
    @ObservationIgnored private let modelContainer: ModelContainer
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "calls")

    /// Connection generation on which each call last attached or dialed.
    @ObservationIgnored private var attachedGeneration: [CallID: Int] = [:]
    @ObservationIgnored private var pendingOutgoingNames: [UUID: String] = [:]
    @ObservationIgnored private var callsNotRecorded: Set<CallID> = []
    @ObservationIgnored private var lastSend: Task<Void, Never>?
    @ObservationIgnored private var clearTask: Task<Void, Never>?
    @ObservationIgnored private var mediaRecoveryTask: Task<Void, Never>?

    /// How long the bridge may take to confirm a call before it fails.
    static let attachTimeout: Duration = .seconds(10)
    /// How long a failed media path may take to recover (the bridge sends
    /// ICE-restart offers; a reconnect re-attaches) before the call ends.
    static let mediaRecoveryTimeout: Duration = .seconds(15)

    init(bridge: BridgeConnection, contacts: ContactsDirectory, modelContainer: ModelContainer) {
        self.bridge = bridge
        self.contacts = contacts
        self.modelContainer = modelContainer
        provider = CXProvider(configuration: Self.makeProviderConfiguration())
        super.init()

        provider.setDelegate(self, queue: nil)
        pushRegistry.delegate = self
        updatePushRegistration(paired: bridge.isPaired)

        bridge.onMessage = { [weak self] message in self?.handle(message) }
        bridge.onConnected = { [weak self] in self?.bridgeDidConnect() }
        bridge.onPairingChanged = { [weak self] paired in self?.updatePushRegistration(paired: paired) }
        media.onConnectionStateChange = { [weak self] callId, state in self?.mediaStateChanged(state, for: callId) }
    }

    var hasActiveCall: Bool { activeCall?.isActive == true }

    /// Only a paired device asks for VoIP pushes. Unpairing drops the
    /// token, so APNs answers the bridge with 410 and the bridge forgets it.
    private func updatePushRegistration(paired: Bool) {
        pushRegistry.desiredPushTypes = paired ? [.voIP] : []
    }

    // MARK: - User actions

    /// Starts an outgoing call through CallKit. Returns `false` and sets
    /// `failure` if the call cannot start.
    @discardableResult
    func startCall(to rawNumber: String, name: String? = nil) async -> Bool {
        guard let number = PhoneNumber.dialable(rawNumber) else {
            failure = .invalidNumber
            return false
        }
        guard bridge.isPaired, bridge.status != .rejected else {
            failure = .notPaired
            return false
        }
        guard !hasActiveCall else {
            failure = .callInProgress
            return false
        }
        // Skip a pending reconnect backoff; dialing waits for the connection.
        bridge.refresh()

        let uuid = UUID()
        pendingOutgoingNames[uuid] = name ?? contacts.name(for: number)
        let action = CXStartCallAction(call: uuid, handle: CXHandle(type: .phoneNumber, value: number))
        do {
            try await callController.request(CXTransaction(action: action))
            return true
        } catch {
            pendingOutgoingNames[uuid] = nil
            logger.error("Start call request failed: \(error.localizedDescription, privacy: .public)")
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

    // MARK: - Bridge messages

    private func handle(_ message: SignalingMessage) {
        switch message {
        case .callIncoming(let incoming):
            if activeCall?.id == incoming.callId {
                apply(.bridgeConfirmedIncoming(incoming), to: incoming.callId)
            } else if !hasActiveCall {
                // Announced over an open connection, e.g. while the app is
                // in the foreground. The push for it may follow; CallKit
                // then rejects the duplicate UUID, which is fine.
                let (session, effects) = CallSession.incoming(announced: incoming, now: .now)
                begin(session)
                reportIncoming(session, completion: nil)
                perform(effects, for: session.id)
            }
        case .callOffer(let offer):
            apply(.offer(offer), to: offer.callId)
        case .callState(let change):
            apply(.remoteState(change.state), to: change.callId)
        case .callEnded(let ended):
            apply(.bridgeEnded(ended.reason), to: ended.callId)
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
        mediaState = nil
        scheduleAttachTimeout(for: session.id)
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
                attach(callId)
            case .sendAccept:
                send(.callAccept(CallReference(callId: callId)))
            case .sendHangup(let reason):
                send(.callHangup(Hangup(callId: callId, reason: reason)))
            case .negotiate(let offer):
                negotiate(offer)
            case .startWebSocketMedia:
                // The iPhone announces only WebRTC and never feeds
                // `call.media` into the session; this is the watch's path.
                logger.error("Unexpected WebSocket media on the iPhone")
            case .updateRemoteParty(let number, let name):
                provider.reportCall(with: callId.uuid, updated: callUpdate(number: number, name: name))
            case .reportOutgoingConnected:
                provider.reportOutgoingCall(with: callId.uuid, connectedAt: nil)
            case .reportEnded(let reason):
                provider.reportCall(with: callId.uuid, endedAt: nil, reason: reason.cxReason)
            case .startRingback:
                ringback.start()
            case .stopRingback:
                ringback.stop()
            case .closeMedia:
                media.close(callId: callId)
            }
        }
    }

    private func finish(_ call: CallSession) {
        mediaRecoveryTask?.cancel()
        mediaRecoveryTask = nil
        ringback.stop()
        isMuted = false
        attachedGeneration[call.id] = nil
        if callsNotRecorded.remove(call.id) == nil, let record = CallRecord(session: call) {
            let context = modelContainer.mainContext
            context.insert(record)
            do {
                try context.save()
            } catch {
                logger.error("Saving call record failed: \(error.localizedDescription, privacy: .public)")
            }
        }
        // Keep the ended call on screen briefly so the user sees the outcome.
        clearTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled, let self, self.activeCall?.id == call.id else { return }
            self.activeCall = nil
            self.mediaState = nil
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

    private func negotiate(_ offer: SessionOffer) {
        Task {
            do {
                let sdp = try await media.answer(offer)
                guard activeCall?.id == offer.callId, activeCall?.isActive == true else { return }
                await send(.callAnswer(SessionAnswer(callId: offer.callId, sdp: sdp))).value
                apply(.localAnswerSent, to: offer.callId)
            } catch {
                logger.error("Negotiation failed: \(error.localizedDescription, privacy: .public)")
                send(.callHangup(Hangup(callId: offer.callId, reason: .failed)))
                apply(.bridgeEnded(.failed), to: offer.callId)
            }
        }
    }

    private func mediaStateChanged(_ state: MediaEngine.ConnectionState, for callId: CallID) {
        guard activeCall?.id == callId else { return }
        mediaState = state
        switch state {
        case .interrupted, .failed:
            // A dead socket reconnects now; the re-attach brings a fresh
            // ICE-restart offer. With a live socket the bridge sends one.
            bridge.refresh()
            if state == .failed { scheduleMediaRecoveryTimeout(for: callId) }
        case .connected:
            mediaRecoveryTask?.cancel()
            mediaRecoveryTask = nil
        case .connecting:
            break
        }
    }

    private func scheduleMediaRecoveryTimeout(for callId: CallID) {
        guard mediaRecoveryTask == nil else { return }
        mediaRecoveryTask = Task { [weak self] in
            try? await Task.sleep(for: Self.mediaRecoveryTimeout)
            guard !Task.isCancelled, let self else { return }
            self.mediaRecoveryTask = nil
            guard self.activeCall?.id == callId, self.activeCall?.isActive == true, self.mediaState != .connected else { return }
            self.logger.error("Media did not recover; ending call")
            self.send(.callHangup(Hangup(callId: callId, reason: .failed)))
            self.apply(.bridgeEnded(.failed), to: callId)
        }
    }

    // MARK: - CallKit helpers

    private func reportIncoming(_ session: CallSession, completion: (@Sendable () -> Void)?) {
        let callId = session.id
        let update = callUpdate(number: session.remoteNumber, name: session.remoteName)
        provider.reportNewIncomingCall(with: callId.uuid, update: update) { [weak self] error in
            Task { @MainActor in
                defer { completion?() }
                guard let error, let self else { return }
                self.incomingReportFailed(callId, error: error)
            }
        }
    }

    private func incomingReportFailed(_ callId: CallID, error: any Error) {
        if let callKitError = error as? CXErrorCodeIncomingCallError, callKitError.code == .callUUIDAlreadyExists {
            return
        }
        // Do Not Disturb, blocked number or another call in progress:
        // tell the bridge this device is out, and keep it off the recents.
        logger.info("CallKit refused incoming call: \(error.localizedDescription, privacy: .public)")
        guard activeCall?.id == callId else { return }
        callsNotRecorded.insert(callId)
        apply(.userEnded, to: callId)
    }

    private func callUpdate(number: String, name: String?) -> CXCallUpdate {
        let update = CXCallUpdate()
        let trimmed = number.trimmingCharacters(in: .whitespaces)
        update.remoteHandle = trimmed.isEmpty
            ? CXHandle(type: .generic, value: String(localized: "Unbekannt"))
            : CXHandle(type: .phoneNumber, value: trimmed)
        update.localizedCallerName = contacts.name(for: trimmed) ?? name
        update.hasVideo = false
        update.supportsDTMF = true
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        return update
    }

    private static func makeProviderConfiguration() -> CXProviderConfiguration {
        let configuration = CXProviderConfiguration()
        configuration.supportsVideo = false
        configuration.maximumCallGroups = 1
        configuration.maximumCallsPerCallGroup = 1
        configuration.supportedHandleTypes = [.phoneNumber, .generic]
        configuration.includesCallsInRecents = true
        configuration.iconTemplateImageData = callKitIcon()
        return configuration
    }

    /// Monochrome template shown on the CallKit call screen.
    private static func callKitIcon() -> Data? {
        let size = CGSize(width: 40, height: 40)
        let symbol = UIImage(systemName: "house.fill", withConfiguration: UIImage.SymbolConfiguration(pointSize: 28, weight: .semibold))
        return UIGraphicsImageRenderer(size: size).image { _ in
            guard let symbol else { return }
            let origin = CGPoint(x: (size.width - symbol.size.width) / 2, y: (size.height - symbol.size.height) / 2)
            symbol.withTintColor(.white).draw(at: origin)
        }.pngData()
    }
}

// MARK: - PushKit

extension CallCenter: @preconcurrency PKPushRegistryDelegate {
    func pushRegistry(_ registry: PKPushRegistry, didUpdate pushCredentials: PKPushCredentials, for type: PKPushType) {
        guard type == .voIP else { return }
        let token = pushCredentials.token.map { String(format: "%02x", $0) }.joined()
        bridge.updatePushToken(token)
    }

    func pushRegistry(_ registry: PKPushRegistry, didInvalidatePushTokenFor type: PKPushType) {
        guard type == .voIP else { return }
        bridge.updatePushToken(nil)
    }

    func pushRegistry(_ registry: PKPushRegistry, didReceiveIncomingPushWith payload: PKPushPayload, for type: PKPushType, completion: @escaping () -> Void) {
        let done = UncheckedSendable(completion)
        guard type == .voIP else {
            completion()
            return
        }

        let push: IncomingCallPush
        do {
            push = try IncomingCallPush(dictionary: payload.dictionaryPayload)
        } catch {
            logger.error("Unusable VoIP push: \(String(describing: error), privacy: .public)")
            let update = CXCallUpdate()
            update.remoteHandle = CXHandle(type: .generic, value: String(localized: "Unbekannt"))
            reportAndEndImmediately(UUID(), update: update, completion: done)
            return
        }

        guard bridge.isPaired else {
            // A push that was already on its way when the user unpaired.
            reportAndEndImmediately(push.callId.uuid, update: callUpdate(number: push.caller, name: push.callerName), completion: done)
            return
        }

        if hasActiveCall && activeCall?.id != push.callId {
            // Busy with another call; CallKit refuses the second group and
            // the bridge keeps ringing the other devices.
            let update = callUpdate(number: push.caller, name: push.callerName)
            provider.reportNewIncomingCall(with: push.callId.uuid, update: update) { _ in done.value() }
            return
        }

        if activeCall?.id == push.callId {
            // Already announced over the open connection.
            provider.reportNewIncomingCall(with: push.callId.uuid, update: callUpdate(number: push.caller, name: push.callerName)) { _ in
                done.value()
            }
            return
        }

        let (session, effects) = CallSession.incoming(push: push, now: .now)
        begin(session)
        reportIncoming(session) { done.value() }
        bridge.refresh()
        perform(effects, for: session.id)
    }

    /// Every VoIP push must produce a CallKit call, or iOS stops delivering
    /// them. For pushes that can't become a real call, report one and end
    /// it right away.
    private func reportAndEndImmediately(_ uuid: UUID, update: CXCallUpdate, completion: UncheckedSendable<() -> Void>) {
        provider.reportNewIncomingCall(with: uuid, update: update) { [weak self] _ in
            Task { @MainActor in
                self?.provider.reportCall(with: uuid, endedAt: nil, reason: .failed)
                completion.value()
            }
        }
    }
}

// MARK: - CallKit

extension CallCenter: @preconcurrency CXProviderDelegate {
    func providerDidReset(_ provider: CXProvider) {
        logger.info("Provider reset")
        ringback.stop()
        if let call = activeCall {
            media.close(callId: call.id)
            if call.isActive { send(.callHangup(Hangup(callId: call.id, reason: .failed))) }
        }
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

        media.configureAudioSession()
        begin(CallSession.outgoing(id: callId, number: number, name: name, now: .now))
        action.fulfill()
        provider.reportOutgoingCall(with: action.callUUID, startedConnectingAt: nil)
        if name != nil {
            provider.reportCall(with: action.callUUID, updated: callUpdate(number: number, name: name))
        }
        dial(callId, number: number)
    }

    func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) {
        guard let call = activeCall, call.id.uuid == action.callUUID, call.isActive else {
            action.fail()
            return
        }
        media.configureAudioSession()
        apply(.userAnswered, to: call.id)
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        if let call = activeCall, call.id.uuid == action.callUUID {
            apply(.userEnded, to: call.id)
        }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        media.setMuted(action.isMuted)
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
        // Holding is not part of protocol v1.
        action.fail()
    }

    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        media.audioSessionDidActivate(audioSession)
        ringback.audioSessionDidActivate()
    }

    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        media.audioSessionDidDeactivate(audioSession)
        ringback.audioSessionDidDeactivate()
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

/// Carries a non-Sendable value (a completion handler from a delegate
/// method) across a callback that is known to run on the same thread.
struct UncheckedSendable<Value>: @unchecked Sendable {
    let value: Value

    init(_ value: Value) {
        self.value = value
    }
}
