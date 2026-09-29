import Foundation
import HousephoneKit
import os
import WatchConnectivity

/// The watch side of WatchConnectivity: receives pairing codes from the
/// iPhone and keeps the iPhone informed about the watch's pairing state.
@MainActor
final class PhoneLink: NSObject {
    private let bridge: WatchBridge
    private let logger = Logger(subsystem: "com.jorisconrad.housephone.watch", category: "phone")

    init(bridge: WatchBridge) {
        self.bridge = bridge
        super.init()
        bridge.onStateChange = { [weak self] state in self?.publish(state) }
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    /// The latest state, in the application context (survives restarts).
    func publish(_ state: WatchPairingState) {
        let session = WCSession.default
        guard session.activationState == .activated else { return }
        do {
            try session.updateApplicationContext(state.dictionary)
        } catch {
            logger.error("Updating application context failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    fileprivate func activated() {
        publish(bridge.pairingState)
    }

    fileprivate func handle(_ instruction: CompanionPairingInstruction, reply: UncheckedReply?) {
        Task {
            let state = await bridge.pair(instruction)
            reply?.send(state.dictionary)
        }
    }
}

/// A WatchConnectivity reply handler, which is safe to call from any thread.
struct UncheckedReply: @unchecked Sendable {
    let handler: ([String: Any]) -> Void

    func send(_ message: [String: Any]) {
        handler(message)
    }
}

extension PhoneLink: WCSessionDelegate {
    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: (any Error)?) {
        guard activationState == .activated else { return }
        Task { @MainActor in self.activated() }
    }

    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        guard let instruction = CompanionPairingInstruction(dictionary: message) else {
            replyHandler([:])
            return
        }
        let reply = UncheckedReply(handler: replyHandler)
        Task { @MainActor in self.handle(instruction, reply: reply) }
    }

    nonisolated func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        guard let instruction = CompanionPairingInstruction(dictionary: userInfo) else { return }
        Task { @MainActor in self.handle(instruction, reply: nil) }
    }
}
