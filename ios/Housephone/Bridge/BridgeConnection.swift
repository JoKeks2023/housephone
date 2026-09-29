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

    /// Why the connection stopped although the device is paired. Status is
    /// `.offline` then; retried on `refresh()` (app active, push), never
    /// without the bridge check.
    enum Problem: Equatable {
        /// The bridge could not prove it holds the pinned key.
        case untrustedBridge
        /// The bridge rejected this device's clock.
        case clockSkew
    }

    private(set) var credentials: BridgeCredentials?
    private(set) var status: Status
    private(set) var welcome: Welcome?
    private(set) var pushToken: String?
    private(set) var problem: Problem?
    /// The bridge reported a newly paired device; shown until dismissed.
    private(set) var newlyPairedDevice: DevicePaired?
    /// Increases with every successful (re)connection. Calls use it to
    /// know whether they still need to attach on the current connection.
    private(set) var generation = 0
    /// The current connection runs over the bridge's private listener
    /// (home Wi-Fi or Tailscale). Pairing the watch needs it.
    private(set) var isOnHomeNetwork = false

    /// Called for every message from the bridge.
    @ObservationIgnored var onMessage: ((SignalingMessage) -> Void)?
    /// Called after every successful (re)connection.
    @ObservationIgnored var onConnected: (() -> Void)?
    /// Called when this device gets paired or unpaired.
    @ObservationIgnored var onPairingChanged: ((Bool) -> Void)?

    @ObservationIgnored private let store: any CredentialStore
    @ObservationIgnored private let keyStore: any DeviceKeyStore
    @ObservationIgnored private var client: SignalingClient?
    @ObservationIgnored private var eventsTask: Task<Void, Never>?
    @ObservationIgnored private var companionWaiter: CheckedContinuation<CompanionPairing, any Error>?
    @ObservationIgnored private var companionTimeout: Task<Void, Never>?
    @ObservationIgnored private let pathObserver = NetworkPathObserver()
    @ObservationIgnored private var pathStarted = false
    @ObservationIgnored private var lastPath: NetworkPathInfo?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "bridge")

    init(store: any CredentialStore, keyStore: any DeviceKeyStore = KeychainDeviceKeyStore()) {
        self.store = store
        self.keyStore = keyStore
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
        let client = SignalingClient(credentials: credentials, hello: makeHello(), keyStore: keyStore, route: BridgeRouteChooser())
        self.client = client
        startPathObserver()
        status = .connecting
        eventsTask = Task { [weak self] in
            for await event in client.events {
                self?.handle(event)
            }
        }
        let path = lastPath
        Task {
            // The first attempt already knows the network.
            if let path { await client.networkPathChanged(path) }
            await client.start()
        }
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

    /// Pairs over `POST /v1/pair` with a new device key. Throws
    /// `HP2Error.bridgeIdentityMismatch` if the bridge doesn't hold the key
    /// from the link's fingerprint. The old pairing stays until the new one
    /// is saved; then its key is deleted.
    func pair(with link: PairingLink) async throws {
        let device = UIDevice.current
        let credentials = try await BridgeHTTPClient(keyStore: keyStore).pair(
            link: link,
            deviceName: device.name,
            platform: .ios,
            model: Self.hardwareModel
        )
        do {
            try store.save(credentials)
        } catch {
            try? keyStore.deleteKey(tag: credentials.keyTag)
            throw error
        }
        let previousKeyTag = self.credentials?.keyTag
        stopClient()
        self.credentials = credentials
        problem = nil
        newlyPairedDevice = nil
        if let previousKeyTag, previousKeyTag != credentials.keyTag {
            try? keyStore.deleteKey(tag: previousKeyTag)
        }
        start()
        onPairingChanged?(true)
    }

    func dismissNewlyPairedDevice() {
        newlyPairedDevice = nil
    }

    /// Asks the bridge to forget this device (best effort, at most 3 s),
    /// then removes the local credentials. Works offline too: the bridge
    /// then drops the push token once APNs reports it as unregistered.
    func unpair() async {
        if isOnline, let client {
            let sent = await Self.attempt(within: .seconds(3)) {
                try await client.send(.deviceUnpair)
            }
            if !sent { logger.info("Bridge not told about unpairing; continuing locally") }
        }
        stopClient()
        try? store.delete()
        if let keyTag = credentials?.keyTag {
            try? keyStore.deleteKey(tag: keyTag)
        }
        credentials = nil
        welcome = nil
        isOnHomeNetwork = false
        pushToken = nil
        problem = nil
        newlyPairedDevice = nil
        status = .unpaired
        onPairingChanged?(false)
    }

    // MARK: - Companion pairing (Apple Watch)

    enum CompanionPairingError: Error, Equatable {
        case alreadyInProgress
    }

    /// Asks the bridge for a one-time pairing code for another device
    /// (`pair.companion.request` → `pair.companion`), e.g. the Apple Watch.
    func requestCompanionPairing(deviceName: String, platform: DevicePlatform = .watchos) async throws -> CompanionPairing {
        _ = try await ensureConnected(timeout: .seconds(10))
        guard companionWaiter == nil else { throw CompanionPairingError.alreadyInProgress }
        return try await withCheckedThrowingContinuation { continuation in
            companionWaiter = continuation
            companionTimeout = Task { [weak self] in
                try? await Task.sleep(for: .seconds(10))
                guard !Task.isCancelled else { return }
                self?.finishCompanionRequest(.failure(SignalingClientError.timeout))
            }
            Task {
                do {
                    try await send(.pairCompanionRequest(CompanionPairingRequest(deviceName: deviceName, platform: platform)))
                } catch {
                    finishCompanionRequest(.failure(error))
                }
            }
        }
    }

    private func finishCompanionRequest(_ result: Result<CompanionPairing, any Error>) {
        companionTimeout?.cancel()
        companionTimeout = nil
        guard let waiter = companionWaiter else { return }
        companionWaiter = nil
        waiter.resume(with: result)
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

    /// Network changes (joining the home Wi-Fi, leaving it, Tailscale on or
    /// off) go to the current client, which moves the connection to the
    /// better route; calls re-attach through `onConnected`.
    private func startPathObserver() {
        guard !pathStarted else { return }
        pathStarted = true
        pathObserver.start { [weak self] path in
            Task { @MainActor in
                guard let self else { return }
                self.lastPath = path
                guard let client = self.client else { return }
                await client.networkPathChanged(path)
            }
        }
    }

    /// Keeps the private listener announced in `welcome` for the next start.
    private func adoptLanURL(_ lanURL: URL?) {
        guard let lanURL, var updated = credentials, updated.lanURL != lanURL else { return }
        updated.lanURL = lanURL
        do {
            try store.save(updated)
            credentials = updated
        } catch {
            logger.error("Could not save the LAN address: \(String(describing: error), privacy: .public)")
        }
    }

    /// Runs `operation` and reports whether it finished successfully in time.
    private static func attempt(within limit: Duration, _ operation: @escaping @Sendable () async throws -> Void) async -> Bool {
        await withTaskGroup(of: Bool.self) { group in
            group.addTask { (try? await operation()) != nil }
            group.addTask {
                try? await Task.sleep(for: limit)
                return false
            }
            let first = await group.next() ?? false
            group.cancelAll()
            return first
        }
    }

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
                adoptLanURL(welcome.lanUrl)
                problem = nil
                status = .online(sipRegistered: welcome.sipRegistered)
                generation += 1
                logger.info("Connected to \(welcome.bridgeName, privacy: .public) \(welcome.bridgeVersion, privacy: .public)")
                onConnected?()
            case .connecting:
                if !isOnline { status = .connecting }
            case .waitingToReconnect, .disconnected:
                status = .offline
                isOnHomeNetwork = false
                finishCompanionRequest(.failure(SignalingClientError.connectionClosed))
            case .unauthorized:
                status = .rejected
                finishCompanionRequest(.failure(SignalingClientError.unauthorized))
            case .untrustedBridge:
                logger.error("Bridge failed the identity check")
                status = .offline
                problem = .untrustedBridge
                finishCompanionRequest(.failure(SignalingClientError.untrustedBridge))
            case .clockSkew:
                status = .offline
                problem = .clockSkew
                finishCompanionRequest(.failure(SignalingClientError.clockSkew))
            }
        case .route(let url, let viaLAN):
            isOnHomeNetwork = viaLAN
            logger.info("Route: \(viaLAN ? "home network" : "public", privacy: .public) (\(url.host() ?? "", privacy: .public))")
        case .message(let message):
            switch message {
            case .status(let bridgeStatus):
                welcome?.sipRegistered = bridgeStatus.sipRegistered
                status = .online(sipRegistered: bridgeStatus.sipRegistered)
            case .pairCompanion(let pairing):
                finishCompanionRequest(.success(pairing))
                return
            case .devicePaired(let paired):
                newlyPairedDevice = paired
            case .error(let error) where error.callId == nil && companionWaiter != nil:
                finishCompanionRequest(.failure(SignalingClientError.bridge(error)))
            default:
                break
            }
            onMessage?(message)
        }
    }

    private func makeHello() -> Hello {
        Hello(
            appVersion: Self.appVersion,
            platform: .ios,
            pushToken: pushToken,
            pushEnvironment: pushToken == nil ? nil : Self.pushEnvironment,
            mediaCapabilities: [.webRTC],
            pushTopic: Self.voipPushTopic
        )
    }

    static var appVersion: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "0"
        let build = info?["CFBundleVersion"] as? String ?? "0"
        return "\(version) (\(build))"
    }

    /// APNs topic of this app's VoIP pushes.
    static let voipPushTopic = "com.jorisconrad.housephone.voip"

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
