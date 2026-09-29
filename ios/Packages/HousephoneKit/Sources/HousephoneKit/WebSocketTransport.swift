import Foundation

public enum WebSocketTransportError: Error, Equatable, Sendable {
    /// The bridge rejected the credentials (HTTP 401 on upgrade).
    case unauthorized
    /// 401, and the bridge's clock differs from ours by more than a minute:
    /// the signed timestamp was outside the bridge's window.
    case clockSkew
    /// The upgrade failed with another HTTP status.
    case httpStatus(Int)
    case closed
}

/// One WebSocket message. Text carries JSON signaling; binary carries call
/// audio for devices using `websocket-pcma` (signaling v1.1).
public enum WebSocketMessage: Sendable, Equatable {
    case text(String)
    case binary(Data)
}

/// A WebSocket connection. Abstracted so the signaling client is testable
/// without a network.
public protocol WebSocketTransport: Sendable {
    func send(_ text: String) async throws
    func send(binary data: Data) async throws
    func receive() async throws -> WebSocketMessage
    func sendPing() async throws
    func close()
    /// A header of the upgrade (`101`) response, e.g. `HP2-Bridge`.
    func upgradeHeader(_ name: String) -> String?
}

public protocol WebSocketTransportFactory: Sendable {
    func connect(to url: URL, headers: [String: String]) async throws -> any WebSocketTransport
}

/// `URLSessionWebSocketTask`-backed transport.
public struct URLSessionWebSocketFactory: WebSocketTransportFactory {
    private let session: URLSession

    public init(session: URLSession = .shared) {
        self.session = session
    }

    public func connect(to url: URL, headers: [String: String]) async throws -> any WebSocketTransport {
        var request = URLRequest(url: url)
        request.timeoutInterval = 10
        for (name, value) in headers {
            request.setValue(value, forHTTPHeaderField: name)
        }
        let task = session.webSocketTask(with: request)
        task.maximumMessageSize = 1 << 20
        task.resume()

        let transport = URLSessionWebSocket(task: task)
        do {
            // The ping completes only after the upgrade succeeded, so it
            // surfaces 401s and unreachable hosts right here.
            try await transport.sendPing()
        } catch {
            task.cancel(with: .goingAway, reason: nil)
            if let response = task.response as? HTTPURLResponse, response.statusCode != 101 {
                guard response.statusCode == 401 else { throw WebSocketTransportError.httpStatus(response.statusCode) }
                // The body of a failed upgrade isn't readable here; the Date
                // header tells a wrong clock from a rejected device.
                throw Self.isClockSkewed(serverDate: response.value(forHTTPHeaderField: "Date"))
                    ? WebSocketTransportError.clockSkew
                    : WebSocketTransportError.unauthorized
            }
            throw error
        }
        return transport
    }

    /// Whether an HTTP `Date` header is more than 60 s away from our clock.
    static func isClockSkewed(serverDate: String?, now: Date = Date()) -> Bool {
        guard let serverDate else { return false }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "GMT")
        formatter.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
        guard let date = formatter.date(from: serverDate) else { return false }
        return abs(date.timeIntervalSince(now)) > 60
    }
}

final class URLSessionWebSocket: WebSocketTransport, @unchecked Sendable {
    // URLSessionWebSocketTask is thread-safe; the class holds no other state.
    private let task: URLSessionWebSocketTask

    init(task: URLSessionWebSocketTask) {
        self.task = task
    }

    func send(_ text: String) async throws {
        try await task.send(.string(text))
    }

    func send(binary data: Data) async throws {
        try await task.send(.data(data))
    }

    func receive() async throws -> WebSocketMessage {
        switch try await task.receive() {
        case .string(let text):
            return .text(text)
        case .data(let data):
            return .binary(data)
        @unknown default:
            throw WebSocketTransportError.closed
        }
    }

    func sendPing() async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            task.sendPing { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume()
                }
            }
        }
    }

    func close() {
        task.cancel(with: .normalClosure, reason: nil)
    }

    func upgradeHeader(_ name: String) -> String? {
        (task.response as? HTTPURLResponse)?.value(forHTTPHeaderField: name)
    }
}
