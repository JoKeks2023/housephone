import Foundation
import HousephoneKit
import Observation
import os

/// The Mac app's connection to the bridge: pairing, credentials and the
/// signaling WebSocket. A reduced `BridgeConnection` (iPhone): no push
/// token (macOS has no PushKit, so calls arrive only while the app runs)
/// and no companion pairing.
@MainActor
@Observable
final class MacBridge {
    enum Status: Equatable {
        case unpaired
        case connecting
        case online(sipRegistered: Bool)
        case offline
        /// The bridge no longer accepts this device (e.g. it was removed).
        case rejected
    }

    /// Why a paired device is offline although the network works.
    enum Problem: Equatable {
        case untrustedBridge
        case clockSkew
    }

    private(set) var credentials: BridgeCredentials?
    private(set) var status: Status
    private(set) var welcome: Welcome?
    private(set) var problem: Problem?
    /// The connection runs over the bridge's private listener.
    private(set) var isOnHomeNetwork = false
    /// Increases with every successful (re)connection.
    private(set) var generation = 0

    /// Called for every message from the bridge (calls, phase 3).
    @ObservationIgnored var onMessage: ((SignalingMessage) -> Void)?

    @ObservationIgnored private let store: any CredentialStore
    @ObservationIgnored private let keyStore: any DeviceKeyStore
    @ObservationIgnored private var client: SignalingClient?
    @ObservationIgnored private var eventsTask: Task<Void, Never>?
    @ObservationIgnored private let pathObserver = NetworkPathObserver()
    @ObservationIgnored private var pathStarted = false
    @ObservationIgnored private var lastPath: NetworkPathInfo?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone.mac", category: "bridge")

    init(store: any CredentialStore = KeychainCredentialStore(), keyStore: any DeviceKeyStore = KeychainDeviceKeyStore()) {
        self.store = store
        self.keyStore = keyStore
        let stored = try? store.load()
        credentials = stored
        status = stored == nil ? .unpaired : .connecting
    }

    var isPaired: Bool { credentials != nil }

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
            if let path { await client.networkPathChanged(path) }
            await client.start()
        }
    }

    /// The app became active or the Mac woke up: reconnect now instead of
    /// waiting for the backoff.
    func refresh() {
        guard let client else {
            start()
            return
        }
        Task { await client.refreshConnection() }
    }

    func send(_ message: SignalingMessage) async throws {
        guard let client else { throw SignalingClientError.notConnected }
        try await client.send(message)
    }

    // MARK: - Pairing

    /// Pairs over `POST /v1/pair` with a new device key. The old pairing
    /// stays until the new one is saved.
    func pair(with link: PairingLink) async throws {
        let credentials = try await BridgeHTTPClient(keyStore: keyStore).pair(
            link: link,
            deviceName: Self.deviceName,
            platform: .macos,
            model: Self.hardwareModel
        )
        try adopt(credentials)
    }

    private func adopt(_ credentials: BridgeCredentials) throws {
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
        if let previousKeyTag, previousKeyTag != credentials.keyTag {
            try? keyStore.deleteKey(tag: previousKeyTag)
        }
        start()
    }

    /// Asks the bridge to forget this Mac (best effort, at most 3 s), then
    /// removes the local credentials.
    func unpair() async {
        if case .online = status, let client {
            _ = await withTaskGroup(of: Bool.self) { group in
                group.addTask { (try? await client.send(.deviceUnpair)) != nil }
                group.addTask {
                    try? await Task.sleep(for: .seconds(3))
                    return false
                }
                let first = await group.next() ?? false
                group.cancelAll()
                return first
            }
        }
        stopClient()
        try? store.delete()
        if let keyTag = credentials?.keyTag {
            try? keyStore.deleteKey(tag: keyTag)
        }
        credentials = nil
        welcome = nil
        isOnHomeNetwork = false
        problem = nil
        status = .unpaired
    }

    // MARK: - Private

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
            case .connecting:
                if case .online = status {} else { status = .connecting }
            case .waitingToReconnect, .disconnected:
                status = .offline
                isOnHomeNetwork = false
            case .unauthorized:
                status = .rejected
            case .untrustedBridge:
                logger.error("Bridge failed the identity check")
                status = .offline
                problem = .untrustedBridge
            case .clockSkew:
                status = .offline
                problem = .clockSkew
            }
        case .route(let url, let viaLAN):
            isOnHomeNetwork = viaLAN
            logger.info("Route: \(viaLAN ? "home network" : "public", privacy: .public) (\(url.host() ?? "", privacy: .public))")
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
            platform: .macos,
            // No video, no WebRTC yet: audio runs over the WebSocket (v1.4).
            mediaCapabilities: [.webSocketPCMA]
        )
    }

    static var appVersion: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "0"
        let build = info?["CFBundleVersion"] as? String ?? "0"
        return "\(version) (\(build))"
    }

    static var deviceName: String {
        Host.current().localizedName ?? ProcessInfo.processInfo.hostName
    }

    /// e.g. `Mac15,6`.
    static var hardwareModel: String {
        var size = 0
        sysctlbyname("hw.model", nil, &size, nil, 0)
        guard size > 0 else { return "Mac" }
        var buffer = [CChar](repeating: 0, count: size)
        sysctlbyname("hw.model", &buffer, &size, nil, 0)
        return String(cString: buffer)
    }
}
