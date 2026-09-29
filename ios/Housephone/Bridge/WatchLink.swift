import Foundation
import HousephoneKit
import Observation
import os
import WatchConnectivity

/// The iPhone side of the Apple Watch: WatchConnectivity status, the
/// watch's pairing state, "Apple Watch koppeln", and unpairing the watch
/// together with the iPhone.
///
/// Pairing: the bridge issues a companion code (`pair.companion`), the
/// iPhone hands it to the watch, and the watch pairs itself over HTTPS.
/// The watch reports back through the application context.
@MainActor
@Observable
final class WatchLink: NSObject {
    enum PairingPhase: Equatable {
        case idle
        case requestingCode
        case sendingToWatch
        /// The watch app isn't open; the code waits in the transfer queue.
        case waitingForWatch(expiresAt: Date)
        case failed(String)
    }

    let isSupported = WCSession.isSupported()
    private(set) var isActivated = false
    private(set) var isWatchPaired = false
    private(set) var isAppInstalled = false
    private(set) var isReachable = false
    private(set) var watchState: WatchPairingState?
    private(set) var pairing: PairingPhase = .idle

    @ObservationIgnored private let bridge: BridgeConnection
    @ObservationIgnored private var lastAutoPairAttempt: Date?
    /// Minimum gap between automatic attempts; the bridge allows 5 companion
    /// codes per hour.
    static let autoPairInterval: TimeInterval = 15 * 60
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "watch")

    init(bridge: BridgeConnection) {
        self.bridge = bridge
        super.init()
        guard isSupported else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    var isPairedWithBridge: Bool { watchState?.phase == .paired }

    // MARK: - Pairing

    /// Pairs the watch without any tap once the iPhone is paired and online
    /// and the watch app is installed. Called when the bridge connects and
    /// whenever the watch's state changes (e.g. the app gets installed).
    func autoPairIfNeeded() {
        guard isSupported, isActivated, isWatchPaired, isAppInstalled,
              bridge.isPaired, bridge.isOnline,
              !isPairedWithBridge, watchState?.phase != .pairing, pairing == .idle
        else { return }
        if let last = lastAutoPairAttempt, Date().timeIntervalSince(last) < Self.autoPairInterval { return }
        lastAutoPairAttempt = Date()
        logger.info("Pairing the watch automatically")
        Task { await pairWatch() }
    }

    func pairWatch() async {
        guard isSupported, isActivated, isWatchPaired, isAppInstalled else { return }
        pairing = .requestingCode
        let instruction: CompanionPairingInstruction
        do {
            let code = try await bridge.requestCompanionPairing(deviceName: String(localized: "Apple Watch"))
            // The watch pins the same bridge key as the iPhone.
            guard let credentials = bridge.credentials else { throw SignalingClientError.notConnected }
            instruction = CompanionPairingInstruction(
                pairing: code,
                bridgeName: bridge.welcome?.bridgeName ?? credentials.bridgeName,
                fingerprint: credentials.bridgeFingerprint
            )
        } catch {
            logger.error("Companion code failed: \(String(describing: error), privacy: .public)")
            pairing = .failed(String(localized: "Die Bridge hat keinen Kopplungscode geliefert. Prüfe, ob sie erreichbar ist."))
            return
        }
        // A queued unpair from earlier must not undo this pairing.
        cancelQueuedTransfers(ofType: CompanionUnpairInstruction.messageType)
        deliver(instruction)
    }

    /// The iPhone unpairs, so the watch unpairs too: right away if its app
    /// is reachable, otherwise through the transfer queue the next time it
    /// runs (e.g. woken by a push).
    func unpairWatch() {
        guard isSupported, isActivated, isWatchPaired, isAppInstalled else { return }
        cancelQueuedTransfers(ofType: CompanionPairingInstruction.messageType)
        pairing = .idle
        lastAutoPairAttempt = nil
        let instruction = CompanionUnpairInstruction()
        let session = WCSession.default
        guard session.isReachable else {
            session.transferUserInfo(instruction.dictionary)
            return
        }
        session.sendMessage(instruction.dictionary, replyHandler: { [weak self] reply in
            let state = WatchPairingState(dictionary: reply)
            Task { @MainActor in
                if let state { self?.apply(state) }
            }
        }, errorHandler: { [weak self] error in
            let description = error.localizedDescription
            Task { @MainActor in
                self?.logger.info("Direct unpair failed (\(description, privacy: .public)); queuing for the watch")
                WCSession.default.transferUserInfo(instruction.dictionary)
            }
        })
    }

    private func cancelQueuedTransfers(ofType type: String) {
        for transfer in WCSession.default.outstandingUserInfoTransfers where companionMessageType(of: transfer.userInfo) == type {
            transfer.cancel()
        }
    }

    func cancelPairing() {
        pairing = .idle
    }

    private func deliver(_ instruction: CompanionPairingInstruction) {
        let session = WCSession.default
        guard session.isReachable else {
            session.transferUserInfo(instruction.dictionary)
            pairing = .waitingForWatch(expiresAt: instruction.expiresAt)
            return
        }
        pairing = .sendingToWatch
        let expiresAt = instruction.expiresAt
        session.sendMessage(instruction.dictionary, replyHandler: { [weak self] reply in
            let state = WatchPairingState(dictionary: reply)
            Task { @MainActor in self?.watchReplied(state) }
        }, errorHandler: { [weak self] error in
            let description = error.localizedDescription
            Task { @MainActor in self?.sendFailed(instruction, expiresAt: expiresAt, description: description) }
        })
    }

    private func watchReplied(_ state: WatchPairingState?) {
        guard let state else {
            pairing = .failed(String(localized: "Die Watch hat unerwartet geantwortet. Aktualisiere Housephone auf beiden Geräten."))
            return
        }
        apply(state)
    }

    private func sendFailed(_ instruction: CompanionPairingInstruction, expiresAt: Date, description: String) {
        logger.info("Direct delivery failed (\(description, privacy: .public)); queuing for the watch")
        // The watch app went away mid-message; let the queue carry it.
        WCSession.default.transferUserInfo(instruction.dictionary)
        pairing = .waitingForWatch(expiresAt: expiresAt)
    }

    private func apply(_ state: WatchPairingState) {
        watchState = state
        switch state.phase {
        case .paired:
            pairing = .idle
        case .failed:
            pairing = .failed(state.failure ?? String(localized: "Die Watch konnte sich nicht koppeln."))
        case .pairing, .unpaired:
            break
        }
    }

    private func update(from session: WCSession) {
        isActivated = session.activationState == .activated
        isWatchPaired = session.isPaired
        isAppInstalled = session.isWatchAppInstalled
        isReachable = session.isReachable
        if let state = WatchPairingState(dictionary: session.receivedApplicationContext) {
            apply(state)
        }
    }

    fileprivate func sessionChanged() {
        update(from: WCSession.default)
        autoPairIfNeeded()
    }

    fileprivate func received(_ state: WatchPairingState) {
        apply(state)
    }
}

extension WatchLink: WCSessionDelegate {
    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: (any Error)?) {
        Task { @MainActor in self.sessionChanged() }
    }

    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {
        Task { @MainActor in self.sessionChanged() }
    }

    nonisolated func sessionDidDeactivate(_ session: WCSession) {
        // Switching to another watch: reactivate for the new one.
        session.activate()
    }

    nonisolated func sessionWatchStateDidChange(_ session: WCSession) {
        Task { @MainActor in self.sessionChanged() }
    }

    nonisolated func sessionReachabilityDidChange(_ session: WCSession) {
        Task { @MainActor in self.sessionChanged() }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
        guard let state = WatchPairingState(dictionary: applicationContext) else { return }
        Task { @MainActor in self.received(state) }
    }

    nonisolated func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        guard let state = WatchPairingState(dictionary: userInfo) else { return }
        Task { @MainActor in self.received(state) }
    }
}
