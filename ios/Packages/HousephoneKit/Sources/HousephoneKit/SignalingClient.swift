import Foundation
import os

public enum SignalingClientError: Error, Equatable, Sendable {
    case notConnected
    case timeout
    case unauthorized
    case bridge(SignalingErrorPayload)
    case connectionClosed
    /// The response wasn't signed with the pinned bridge key, or a sealed
    /// frame or body failed authentication. Never falls back to anything
    /// unauthenticated.
    case untrustedBridge
    /// The bridge rejected the request's timestamp: this device's clock is
    /// off by more than a minute.
    case clockSkew
}

/// Keeps one authenticated WebSocket connection to the bridge alive and
/// reconnects with exponential backoff. Messages and state changes arrive
/// on `events`, which has exactly one consumer.
///
/// Signaling v2 (ADR-0004): the upgrade request is signed with the device
/// key, the bridge's `101` must carry a valid signature of the pinned
/// bridge key before anything is sent, and every frame after that is a
/// sealed binary frame.
///
/// Routes: every connection attempt prefers the bridge's private listener
/// (`lanURL`, home network or Tailscale) when `route` finds it reachable,
/// and falls back to the public URL. `networkPathChanged(_:)` moves an open
/// connection over when the better route changes.
public actor SignalingClient {
    public enum ConnectionState: Sendable, Equatable {
        case disconnected
        case connecting
        case connected(Welcome)
        case waitingToReconnect(attempt: Int)
        /// The bridge rejected this device. Only pairing again helps.
        case unauthorized
        /// The bridge failed to prove its identity. Retried only on
        /// `refreshConnection()`, never without the check.
        case untrustedBridge
        /// This device's clock is off. Retried on `refreshConnection()`.
        case clockSkew

        public var welcome: Welcome? {
            if case .connected(let welcome) = self { welcome } else { nil }
        }

        /// The error of a state that stopped the reconnect loop.
        var stopError: SignalingClientError? {
            switch self {
            case .unauthorized: .unauthorized
            case .untrustedBridge: .untrustedBridge
            case .clockSkew: .clockSkew
            default: nil
            }
        }
    }

    public enum Event: Sendable, Equatable {
        case state(ConnectionState)
        case message(SignalingMessage)
        /// The route of the connection that is about to report
        /// `.connected`: the private listener (`viaLAN`) or the public URL.
        case route(url: URL, viaLAN: Bool)
    }

    public struct Configuration: Sendable {
        public var welcomeTimeout: Duration = .seconds(10)
        public var pingInterval: Duration = .seconds(20)
        public var pingTimeout: Duration = .seconds(5)
        public var initialBackoff: Duration = .milliseconds(500)
        public var maximumBackoff: Duration = .seconds(30)

        public init() {}
    }

    public nonisolated let events: AsyncStream<Event>
    private let eventContinuation: AsyncStream<Event>.Continuation
    /// Binary audio messages from the bridge (`websocket-pcma`), in order,
    /// each laid out as `AudioFrame` (type byte `0x01` + 160 bytes).
    /// Separate from `events` so audio never waits behind UI work. Keeps at
    /// most one second; a slow consumer loses the oldest frames.
    public nonisolated let audio: AsyncStream<Data>
    private let audioContinuation: AsyncStream<Data>.Continuation

    public let credentials: BridgeCredentials
    /// The private listener; starts from the credentials and follows
    /// `welcome.lanUrl`.
    public private(set) var lanURL: URL?
    /// The URL of the current (or last) connection.
    public private(set) var activeURL: URL?
    private let route: BridgeRouteChooser?
    private var path: NetworkPathInfo?
    /// Set when a route change closed the connection on purpose: reconnect
    /// at once instead of backing off.
    private var reconnectNow = false
    private let factory: any WebSocketTransportFactory
    private let keyStore: any DeviceKeyStore
    private let now: @Sendable () -> Date
    private let configuration: Configuration
    private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "signaling")

    public private(set) var state: ConnectionState = .disconnected
    private var hello: Hello
    private var transport: (any WebSocketTransport)?
    private var cipher: HP2FrameCipher?
    private var runTask: Task<Void, Never>?
    private var backoffTask: Task<Void, any Error>?
    private var waiters: [UUID: CheckedContinuation<Welcome, any Error>] = [:]

    public init(
        credentials: BridgeCredentials,
        hello: Hello,
        keyStore: any DeviceKeyStore = KeychainDeviceKeyStore(),
        factory: any WebSocketTransportFactory = URLSessionWebSocketFactory(),
        route: BridgeRouteChooser? = nil,
        configuration: Configuration = Configuration(),
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.credentials = credentials
        self.lanURL = credentials.lanURL
        self.route = route
        self.hello = hello
        self.keyStore = keyStore
        self.factory = factory
        self.configuration = configuration
        self.now = now
        (events, eventContinuation) = AsyncStream.makeStream(of: Event.self, bufferingPolicy: .unbounded)
        (audio, audioContinuation) = AsyncStream.makeStream(of: Data.self, bufferingPolicy: .bufferingNewest(50))
    }

    deinit {
        runTask?.cancel()
        eventContinuation.finish()
        audioContinuation.finish()
    }

    // MARK: - Lifecycle

    public func start() {
        guard runTask == nil else { return }
        runTask = Task { await self.run() }
    }

    public func stop() {
        runTask?.cancel()
        runTask = nil
        backoffTask?.cancel()
        closeTransport()
        failWaiters(SignalingClientError.notConnected)
        setState(.disconnected)
    }

    /// Call when the app becomes active or a push arrives: skips a pending
    /// backoff, and checks that an apparently open connection still works
    /// (iOS drops sockets of suspended apps without telling anyone).
    public func refreshConnection() {
        switch state {
        case .waitingToReconnect:
            backoffTask?.cancel()
        case .connected:
            guard let transport else { return }
            let timeout = configuration.pingTimeout
            Task {
                do {
                    try await withTimeout(timeout, onTimeout: { transport.close() }) {
                        try await transport.sendPing()
                    }
                } catch {
                    transport.close()
                }
            }
        case .disconnected, .untrustedBridge, .clockSkew:
            start()
        case .connecting, .unauthorized:
            break
        }
    }

    // MARK: - Routes

    /// Call on every network path change (`NetworkPathObserver`). Joining
    /// the home network (or Tailscale) moves the connection to the private
    /// listener; leaving it moves it to the public URL. Calls re-attach on
    /// the new connection like after any reconnect.
    public func networkPathChanged(_ newPath: NetworkPathInfo) async {
        guard newPath != path else { return }
        path = newPath
        guard newPath.isSatisfied else { return }
        switch state {
        case .waitingToReconnect:
            backoffTask?.cancel()
        case .connected:
            guard let route, let current = activeURL else { return }
            let preferred = await route.url(publicURL: credentials.bridgeURL, lanURL: lanURL, path: newPath)
            // The state may have changed while probing.
            guard case .connected = state, activeURL == current, let transport else { return }
            if preferred != current {
                logger.info("Network changed; switching route")
                reconnectNow = true
                transport.close()
            } else {
                refreshConnection()
            }
        default:
            break
        }
    }

    private func chooseURL() async -> URL {
        guard let route else { return credentials.bridgeURL }
        return await route.url(publicURL: credentials.bridgeURL, lanURL: lanURL, path: path)
    }

    /// Adopts the private listener announced in `welcome`.
    private func adoptLanURL(from welcome: Welcome) {
        guard let url = welcome.lanUrl,
              let scheme = url.scheme?.lowercased(), scheme == "ws" || scheme == "wss",
              url.host()?.isEmpty == false
        else { return }
        lanURL = url
    }

    // MARK: - Sending

    public func send(_ message: SignalingMessage) async throws {
        guard case .connected = state, let transport else { throw SignalingClientError.notConnected }
        let frame = try seal(HP2FrameCipher.jsonPlaintext(SignalingCoding.encode(message)))
        logger.debug("→ \(message.type, privacy: .public)")
        try await transport.send(binary: frame)
    }

    /// Sends one audio message (`websocket-pcma`), laid out as `AudioFrame`.
    public func sendAudio(_ message: Data) async throws {
        guard case .connected = state, let transport else { throw SignalingClientError.notConnected }
        guard message.first == HP2FrameType.audio.rawValue else { return }
        try await transport.send(binary: try seal(message))
    }

    /// Updates the data sent in `hello`. If connected, tells the bridge
    /// right away via `device.update`.
    public func updateHello(_ newHello: Hello) async {
        let changed = newHello.pushToken != hello.pushToken
            || newHello.pushEnvironment != hello.pushEnvironment
            || newHello.mediaCapabilities != hello.mediaCapabilities
            || newHello.pushTopic != hello.pushTopic
            || newHello.pushKey != hello.pushKey
        hello = newHello
        guard changed, case .connected = state else { return }
        let update = DeviceUpdate(
            pushToken: newHello.pushToken,
            pushEnvironment: newHello.pushEnvironment,
            mediaCapabilities: newHello.mediaCapabilities,
            pushTopic: newHello.pushTopic,
            pushKey: newHello.pushKey
        )
        try? await send(.deviceUpdate(update))
    }

    /// Returns once connected, or throws `timeout`, `unauthorized`,
    /// `untrustedBridge` or `clockSkew`.
    public func waitUntilConnected(timeout: Duration) async throws -> Welcome {
        if let welcome = state.welcome { return welcome }
        if state == .unauthorized { throw SignalingClientError.unauthorized }
        if runTask == nil {
            // A new attempt after `.untrustedBridge` or `.clockSkew` checks
            // the bridge from scratch; its outcome decides, not the old state.
            if state.stopError != nil { setState(.connecting) }
            start()
        }
        return try await withTimeout(timeout) {
            try await self.nextConnection()
        }
    }

    private func nextConnection() async throws -> Welcome {
        let id = UUID()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                if Task.isCancelled {
                    continuation.resume(throwing: CancellationError())
                } else if let welcome = state.welcome {
                    continuation.resume(returning: welcome)
                } else if let error = state.stopError {
                    // The attempt may have failed before this waiter registered.
                    continuation.resume(throwing: error)
                } else {
                    waiters[id] = continuation
                }
            }
        } onCancel: {
            Task { await self.cancelWaiter(id) }
        }
    }

    private func cancelWaiter(_ id: UUID) {
        waiters.removeValue(forKey: id)?.resume(throwing: CancellationError())
    }

    private func failWaiters(_ error: any Error) {
        let pending = waiters
        waiters.removeAll()
        for continuation in pending.values { continuation.resume(throwing: error) }
    }

    // MARK: - Connection loop

    private func run() async {
        var attempt = 0
        while !Task.isCancelled {
            setState(.connecting)
            do {
                guard let key = try keyStore.key(tag: credentials.keyTag) else {
                    // Without its key this device can't prove who it is.
                    throw SignalingClientError.unauthorized
                }
                let url = await chooseURL()
                try Task.checkCancellation()
                activeURL = url
                let exchange = try HP2Signer(credentials: credentials, key: key, now: now)
                    .exchange(method: "GET", url: url, body: nil)
                let transport = try await factory.connect(
                    to: url,
                    headers: ["Authorization": exchange.authorization.headerValue]
                )
                // Assign first, so the cleanup below closes it on cancellation.
                self.transport = transport
                try Task.checkCancellation()

                // Nothing leaves this device before the bridge proved it
                // holds the pinned key.
                let keys: HP2SessionKeys
                do {
                    keys = try exchange.verifyResponse(
                        status: 101,
                        bridgeHeader: transport.upgradeHeader(HP2.bridgeHeaderName),
                        body: Data()
                    )
                } catch {
                    throw SignalingClientError.untrustedBridge
                }
                cipher = HP2FrameCipher(keys: keys)

                try await transport.send(binary: try seal(HP2FrameCipher.jsonPlaintext(SignalingCoding.encode(.hello(hello)))))
                let welcome = try await awaitWelcome(on: transport)
                adoptLanURL(from: welcome)
                eventContinuation.yield(.route(url: url, viaLAN: url != credentials.bridgeURL && url == lanURL))

                attempt = 0
                setState(.connected(welcome))
                let pending = waiters
                waiters.removeAll()
                for continuation in pending.values { continuation.resume(returning: welcome) }

                try await receiveMessages(on: transport)
            } catch WebSocketTransportError.unauthorized, SignalingClientError.unauthorized {
                logger.error("Bridge rejected device credentials")
                stopLoop(with: .unauthorized, error: SignalingClientError.unauthorized)
                return
            } catch WebSocketTransportError.clockSkew, SignalingClientError.clockSkew {
                logger.error("Bridge rejected the timestamp: device clock is off")
                stopLoop(with: .clockSkew, error: SignalingClientError.clockSkew)
                return
            } catch SignalingClientError.untrustedBridge {
                logger.fault("Bridge failed to prove its identity")
                stopLoop(with: .untrustedBridge, error: SignalingClientError.untrustedBridge)
                return
            } catch {
                if error as? HP2Error == .invalidFrame {
                    logger.error("Integrity check of a frame failed; reconnecting")
                } else {
                    logger.info("Connection ended: \(String(describing: error), privacy: .public)")
                }
            }

            closeTransport()
            guard !Task.isCancelled else { break }
            if reconnectNow {
                reconnectNow = false
                continue
            }

            attempt += 1
            setState(.waitingToReconnect(attempt: attempt))
            let delay = backoff(forAttempt: attempt)
            let sleep = Task { try await Task.sleep(for: delay) }
            backoffTask = sleep
            _ = try? await sleep.value
            backoffTask = nil
        }
        if runTask != nil { setState(.disconnected) }
    }

    private func stopLoop(with newState: ConnectionState, error: SignalingClientError) {
        closeTransport()
        runTask = nil
        failWaiters(error)
        setState(newState)
    }

    private func closeTransport() {
        transport?.close()
        transport = nil
        cipher = nil
    }

    private func seal(_ plaintext: Data) throws -> Data {
        guard cipher != nil else { throw SignalingClientError.notConnected }
        return try cipher!.seal(plaintext)
    }

    /// The next frame's plaintext. Text frames and frames that fail
    /// authentication end the connection.
    private func receivePlaintext(on transport: any WebSocketTransport) async throws -> Data {
        guard case .binary(let frame) = try await transport.receive() else { throw HP2Error.invalidFrame }
        guard cipher != nil else { throw SignalingClientError.notConnected }
        return try cipher!.open(frame)
    }

    private func decodeJSON(_ plaintext: Data) -> SignalingMessage? {
        guard plaintext.first == HP2FrameType.json.rawValue else { return nil }
        do {
            return try SignalingCoding.decode(String(decoding: plaintext.dropFirst(), as: UTF8.self))
        } catch {
            logger.error("Undecodable message: \(String(describing: error), privacy: .public)")
            return nil
        }
    }

    private func awaitWelcome(on transport: any WebSocketTransport) async throws -> Welcome {
        try await withTimeout(configuration.welcomeTimeout, onTimeout: { transport.close() }) {
            while true {
                let plaintext = try await self.receivePlaintext(on: transport)
                guard let message = await self.decodeJSON(plaintext) else { continue }
                switch message {
                case .welcome(let welcome):
                    return welcome
                case .error(let error) where error.code == .unauthorized:
                    throw SignalingClientError.unauthorized
                default:
                    await self.deliver(message)
                }
            }
        }
    }

    private func receiveMessages(on transport: any WebSocketTransport) async throws {
        let interval = configuration.pingInterval
        let pingTimeout = configuration.pingTimeout
        let pinger = Task {
            while !Task.isCancelled {
                try await Task.sleep(for: interval)
                do {
                    try await withTimeout(pingTimeout, onTimeout: { transport.close() }) {
                        try await transport.sendPing()
                    }
                } catch is CancellationError {
                    return
                } catch {
                    transport.close()
                    return
                }
            }
        }
        defer { pinger.cancel() }

        while true {
            let plaintext = try await receivePlaintext(on: transport)
            switch plaintext.first.flatMap(HP2FrameType.init(rawValue:)) {
            case .json:
                if let message = decodeJSON(plaintext) { deliver(message) }
            case .audio:
                audioContinuation.yield(plaintext)
            case nil:
                // Reserved frame types are ignored.
                continue
            }
        }
    }

    private func deliver(_ message: SignalingMessage) {
        logger.debug("← \(message.type, privacy: .public)")
        if case .status(let status) = message, case .connected(var welcome) = state {
            welcome.sipRegistered = status.sipRegistered
            state = .connected(welcome)
        }
        eventContinuation.yield(.message(message))
    }

    private func setState(_ newState: ConnectionState) {
        guard newState != state else { return }
        state = newState
        eventContinuation.yield(.state(newState))
    }

    private func backoff(forAttempt attempt: Int) -> Duration {
        let base = configuration.initialBackoff * (1 << min(attempt - 1, 10))
        let capped = min(base, configuration.maximumBackoff)
        // ±20 % jitter so several devices don't reconnect in lockstep.
        return capped * Double.random(in: 0.8...1.2)
    }
}

/// Runs `operation`, throwing `SignalingClientError.timeout` after
/// `duration`. `onTimeout` runs first, so it can unblock operations that
/// ignore cancellation (e.g. by closing a socket).
func withTimeout<T: Sendable>(
    _ duration: Duration,
    onTimeout: @escaping @Sendable () -> Void = {},
    operation: @escaping @Sendable () async throws -> T
) async throws -> T {
    try await withThrowingTaskGroup(of: T.self) { group in
        group.addTask { try await operation() }
        group.addTask {
            try await Task.sleep(for: duration)
            onTimeout()
            throw SignalingClientError.timeout
        }
        defer { group.cancelAll() }
        guard let result = try await group.next() else { throw SignalingClientError.timeout }
        return result
    }
}
