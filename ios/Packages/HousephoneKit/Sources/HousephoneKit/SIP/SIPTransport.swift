import Foundation
@preconcurrency import Network

/// Datagram transport between the user agent and one SIP server.
public protocol SIPTransport: Sendable {
    /// Local IP and port the server can reach us at (Via, Contact, SDP).
    /// Nil until the transport is ready.
    func localEndpoint() async -> (host: String, port: UInt16)?
    func send(_ datagram: Data) async throws
    /// Every datagram from the server, until `close()`.
    var incoming: AsyncStream<Data> { get }
    func close()
}

public enum SIPTransportError: Error, Equatable {
    case notReady
    case failed(String)
}

/// UDP to the registrar through Network.framework. One connected flow:
/// requests from the FRITZ!Box arrive on the same 5-tuple because our
/// Contact points at this local port.
public final class UDPSIPTransport: SIPTransport, @unchecked Sendable {
    public let incoming: AsyncStream<Data>
    private let continuation: AsyncStream<Data>.Continuation
    private let connection: NWConnection
    private let queue = DispatchQueue(label: "com.jorisconrad.housephone.sip")
    private let ready = ReadyGate()

    public init(host: String, port: UInt16 = 5060) {
        (incoming, continuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(64))
        let parameters = NWParameters.udp
        // Stay on Wi-Fi/Ethernet: the FRITZ!Box is only reachable at home.
        parameters.prohibitedInterfaceTypes = [.cellular]
        connection = NWConnection(host: NWEndpoint.Host(host), port: NWEndpoint.Port(rawValue: port) ?? 5060, using: parameters)
        connection.stateUpdateHandler = { [ready, continuation] state in
            switch state {
            case .ready: ready.open(nil)
            case .waiting(let error):
                // No route (e.g. not on Wi-Fi); the owner starts over later.
                ready.open(SIPTransportError.failed(error.localizedDescription))
            case .failed(let error):
                ready.open(SIPTransportError.failed(error.localizedDescription))
                continuation.finish()
            case .cancelled:
                ready.open(SIPTransportError.notReady)
                continuation.finish()
            default: break
            }
        }
        connection.start(queue: queue)
        receive()
    }

    private func receive() {
        connection.receiveMessage { [weak self] data, _, _, error in
            guard let self else { return }
            if let data, !data.isEmpty { continuation.yield(data) }
            if error == nil { receive() }
        }
    }

    public func localEndpoint() async -> (host: String, port: UInt16)? {
        guard (try? await ready.wait()) != nil,
              case .hostPort(let host, let port)? = connection.currentPath?.localEndpoint
        else { return nil }
        return (Self.string(host), port.rawValue)
    }

    public func send(_ datagram: Data) async throws {
        try await ready.wait()
        try await withCheckedThrowingContinuation { (resume: CheckedContinuation<Void, any Error>) in
            connection.send(content: datagram, completion: .contentProcessed { error in
                if let error { resume.resume(throwing: SIPTransportError.failed(error.localizedDescription)) } else { resume.resume() }
            })
        }
    }

    public func close() {
        connection.cancel()
        continuation.finish()
    }

    static func string(_ host: NWEndpoint.Host) -> String {
        switch host {
        case .ipv4(let address): "\(address)".components(separatedBy: "%").first ?? "\(address)"
        case .ipv6(let address): "\(address)".components(separatedBy: "%").first ?? "\(address)"
        case .name(let name, _): name
        @unknown default: "\(host)"
        }
    }
}

/// Waits until a connection is ready (or failed), for any number of callers.
final class ReadyGate: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Result<Void, any Error>?
    private var waiters: [CheckedContinuation<Void, any Error>] = []

    func open(_ error: (any Error)?) {
        lock.lock()
        guard result == nil else { lock.unlock(); return }
        let result: Result<Void, any Error> = error.map { .failure($0) } ?? .success(())
        self.result = result
        let waiting = waiters
        waiters = []
        lock.unlock()
        for waiter in waiting { waiter.resume(with: result) }
    }

    func wait() async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            lock.lock()
            if let result {
                lock.unlock()
                continuation.resume(with: result)
            } else {
                waiters.append(continuation)
                lock.unlock()
            }
        }
    }
}
