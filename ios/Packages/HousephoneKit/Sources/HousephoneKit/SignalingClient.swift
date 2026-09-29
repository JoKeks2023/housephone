import Foundation
import os

public enum SignalingClientError: Error, Equatable, Sendable {
    case notConnected
    case timeout
    case unauthorized
    case bridge(SignalingErrorPayload)
    case connectionClosed
}

/// Keeps one authenticated WebSocket connection to the bridge alive and
/// reconnects with exponential backoff. Messages and state changes arrive
/// on `events`, which has exactly one consumer.
public actor SignalingClient {
    public enum ConnectionState: Sendable, Equatable {
        case disconnected
        case connecting
        case connected(Welcome)
        case waitingToReconnect(attempt: Int)
        /// The bridge rejected this device. Only pairing again helps.
        case unauthorized

        public var welcome: Welcome? {
            if case .connected(let welcome) = self { welcome } else { nil }
        }
    }

    public enum Event: Sendable, Equatable {
        case state(ConnectionState)
        case message(SignalingMessage)
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

    public let credentials: BridgeCredentials
    private let factory: any WebSocketTransportFactory
    private let configuration: Configuration
    private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "signaling")

    public private(set) var state: ConnectionState = .disconnected
    private var hello: Hello
    private var transport: (any WebSocketTransport)?
    private var runTask: Task<Void, Never>?
    private var backoffTask: Task<Void, any Error>?
    private var waiters: [UUID: CheckedContinuation<Welcome, any Error>] = [:]

    public init(
        credentials: BridgeCredentials,
        hello: Hello,
        factory: any WebSocketTransportFactory = URLSessionWebSocketFactory(),
        configuration: Configuration = Configuration()
    ) {
        self.credentials = credentials
        self.hello = hello
        self.factory = factory
        self.configuration = configuration
        (events, eventContinuation) = AsyncStream.makeStream(of: Event.self, bufferingPolicy: .unbounded)
    }

    deinit {
        runTask?.cancel()
        eventContinuation.finish()
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
        transport?.close()
        transport = nil
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
        case .disconnected:
            start()
        case .connecting, .unauthorized:
            break
        }
    }

    // MARK: - Sending

    public func send(_ message: SignalingMessage) async throws {
        guard case .connected = state, let transport else { throw SignalingClientError.notConnected }
        let text = try SignalingCoding.encode(message)
        logger.debug("→ \(message.type, privacy: .public)")
        try await transport.send(text)
    }

    /// Updates the data sent in `hello`. If connected, tells the bridge
    /// right away via `device.update`.
    public func updateHello(_ newHello: Hello) async {
        let changed = newHello.pushToken != hello.pushToken || newHello.pushEnvironment != hello.pushEnvironment
        hello = newHello
        guard changed, case .connected = state else { return }
        let update = DeviceUpdate(pushToken: newHello.pushToken, pushEnvironment: newHello.pushEnvironment)
        try? await send(.deviceUpdate(update))
    }

    /// Returns once connected, or throws `timeout`/`unauthorized`.
    public func waitUntilConnected(timeout: Duration) async throws -> Welcome {
        if let welcome = state.welcome { return welcome }
        if state == .unauthorized { throw SignalingClientError.unauthorized }
        if runTask == nil { start() }
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
                } else if state == .unauthorized {
                    // The attempt may have failed before this waiter registered.
                    continuation.resume(throwing: SignalingClientError.unauthorized)
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
                let transport = try await factory.connect(
                    to: credentials.bridgeURL,
                    headers: ["Authorization": credentials.authorizationHeader]
                )
                // Assign first, so the cleanup below closes it on cancellation.
                self.transport = transport
                try Task.checkCancellation()

                try await transport.send(SignalingCoding.encode(.hello(hello)))
                let welcome = try await awaitWelcome(on: transport)

                attempt = 0
                setState(.connected(welcome))
                let pending = waiters
                waiters.removeAll()
                for continuation in pending.values { continuation.resume(returning: welcome) }

                try await receiveMessages(on: transport)
            } catch WebSocketTransportError.unauthorized, SignalingClientError.unauthorized {
                logger.error("Bridge rejected device credentials")
                transport?.close()
                transport = nil
                runTask = nil
                failWaiters(SignalingClientError.unauthorized)
                setState(.unauthorized)
                return
            } catch {
                logger.info("Connection ended: \(String(describing: error), privacy: .public)")
            }

            transport?.close()
            transport = nil
            guard !Task.isCancelled else { break }

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

    private func awaitWelcome(on transport: any WebSocketTransport) async throws -> Welcome {
        try await withTimeout(configuration.welcomeTimeout, onTimeout: { transport.close() }) {
            while true {
                let text = try await transport.receive()
                guard let message = try? SignalingCoding.decode(text) else { continue }
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
            let text = try await transport.receive()
            do {
                deliver(try SignalingCoding.decode(text))
            } catch {
                logger.error("Undecodable message: \(String(describing: error), privacy: .public)")
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

    // MARK: - Pairing

    /// Runs the one-shot pairing exchange: connect without credentials,
    /// send `pair`, wait for `pair.ok`.
    public static func pair(
        link: PairingLink,
        deviceName: String,
        platform: DevicePlatform,
        model: String?,
        factory: any WebSocketTransportFactory = URLSessionWebSocketFactory(),
        timeout: Duration = .seconds(15)
    ) async throws -> BridgeCredentials {
        let transport = try await factory.connect(to: link.bridgeURL, headers: [:])
        defer { transport.close() }

        let request = PairRequest(code: link.code, deviceName: deviceName, platform: platform, model: model)
        try await transport.send(SignalingCoding.encode(.pair(request)))

        let result = try await withTimeout(timeout, onTimeout: { transport.close() }) {
            while true {
                let text: String
                do {
                    text = try await transport.receive()
                } catch {
                    throw SignalingClientError.connectionClosed
                }
                switch try? SignalingCoding.decode(text) {
                case .pairOK(let result):
                    return result
                case .error(let error):
                    throw SignalingClientError.bridge(error)
                default:
                    continue
                }
            }
        }
        return BridgeCredentials(bridgeURL: link.bridgeURL, pairing: result)
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
