import Foundation
import HousephoneKit
import Observation
import os
import UIKit

/// The app's single connection to the bridge: pairing, credentials,
/// connection state, and routing of messages to the call center.
@MainActor
@Observable
final class BridgeConnection {
    enum Status: Equatable {
        case unpaired
        case connecting
        case online(sipRegistered: Bool)
        case offline
        /// The bridge no longer accepts this device (e.g. it was removed).
        case rejected
    }

    private(set) var credentials: BridgeCredentials?
    private(set) var status: Status
    private(set) var welcome: Welcome?
    private(set) var pushToken: String?
    /// Increases with every successful (re)connection. Calls use it to
    /// know whether they still need to attach on the current connection.
    private(set) var generation = 0

    /// Called for every message from the bridge.
    @ObservationIgnored var onMessage: ((SignalingMessage) -> Void)?
    /// Called after every successful (re)connection.
    @ObservationIgnored var onConnected: (() -> Void)?

    @ObservationIgnored private let store: any CredentialStore
    @ObservationIgnored private var client: SignalingClient?
    @ObservationIgnored private var eventsTask: Task<Void, Never>?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "bridge")

    init(store: any CredentialStore) {
        self.store = store
        let stored: BridgeCredentials?
        do {
            stored = try store.load()
        } catch {
            stored = nil
        }
        credentials = stored
        status = stored == nil ? .unpaired : .connecting
    }

    var isPaired: Bool { credentials != nil }

    var isOnline: Bool {
        if case .online = status { true } else { false }
    }

    // MARK: - Lifecycle

    func start() {
        guard let credentials, client == nil else { return }
        let client = SignalingClient(credentials: credentials, hello: makeHello())
        self.client = client
        status = .connecting
        eventsTask = Task { [weak self] in
            for await event in client.events {
                self?.handle(event)
            }
        }
        Task { await client.start() }
    }

    /// App became active or a push arrived: reconnect now instead of
    /// waiting for the backoff, and verify the socket still works.
    func refresh() {
        guard let client else {
            start()
            return
        }
        Task { await client.refreshConnection() }
    }

    func ensureConnected(timeout: Duration = .seconds(10)) async throws -> Welcome {
        if client == nil { start() }
        guard let client else { throw SignalingClientError.notConnected }
        return try await client.waitUntilConnected(timeout: timeout)
    }

    func send(_ message: SignalingMessage) async throws {
        guard let client else { throw SignalingClientError.notConnected }
        try await client.send(message)
    }

    // MARK: - Pairing

    func pair(with link: PairingLink) async throws {
        let device = UIDevice.current
        let credentials = try await SignalingClient.pair(
            link: link,
            deviceName: device.name,
            platform: .ios,
            model: Self.hardwareModel
        )
        try store.save(credentials)
        stopClient()
        self.credentials = credentials
        start()
    }

    func unpair() {
        stopClient()
        try? store.delete()
        credentials = nil
        welcome = nil
        status = .unpaired
    }

    // MARK: - Push token

    func updatePushToken(_ token: String?) {
        guard token != pushToken else { return }
        pushToken = token
        guard let client else { return }
        let hello = makeHello()
        Task { await client.updateHello(hello) }
    }

    // MARK: - Private

    private func stopClient() {
        eventsTask?.cancel()
        eventsTask = nil
        if let client {
            Task { await client.stop() }
        }
        client = nil
    }

    private func handle(_ event: SignalingClient.Event) {
        switch event {
        case .state(let state):
            switch state {
            case .connected(let welcome):
                self.welcome = welcome
                status = .online(sipRegistered: welcome.sipRegistered)
                generation += 1
                logger.info("Connected to \(welcome.bridgeName, privacy: .public) \(welcome.bridgeVersion, privacy: .public)")
                onConnected?()
            case .connecting:
                if !isOnline { status = .connecting }
            case .waitingToReconnect, .disconnected:
                status = .offline
            case .unauthorized:
                status = .rejected
            }
        case .message(let message):
            if case .status(let bridgeStatus) = message {
                welcome?.sipRegistered = bridgeStatus.sipRegistered
                status = .online(sipRegistered: bridgeStatus.sipRegistered)
            }
            onMessage?(message)
        }
    }

    private func makeHello() -> Hello {
        Hello(
            appVersion: Self.appVersion,
            platform: .ios,
            pushToken: pushToken,
            pushEnvironment: pushToken == nil ? nil : Self.pushEnvironment
        )
    }

    static var appVersion: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "0"
        let build = info?["CFBundleVersion"] as? String ?? "0"
        return "\(version) (\(build))"
    }

    /// Debug builds get their push tokens from the APNs sandbox.
    static var pushEnvironment: PushEnvironment {
        #if DEBUG
        .development
        #else
        .production
        #endif
    }

    /// e.g. `iPhone17,1`.
    static var hardwareModel: String {
        var systemInfo = utsname()
        uname(&systemInfo)
        return withUnsafeBytes(of: &systemInfo.machine) { buffer in
            String(decoding: buffer.prefix { $0 != 0 }, as: UTF8.self)
        }
    }
}
