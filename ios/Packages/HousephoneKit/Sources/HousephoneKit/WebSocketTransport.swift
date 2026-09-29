import Foundation

public enum WebSocketTransportError: Error, Equatable, Sendable {
    /// The bridge rejected the credentials (HTTP 401 on upgrade).
    case unauthorized
    /// The upgrade failed with another HTTP status.
    case httpStatus(Int)
    case unexpectedBinaryMessage
    case closed
}

/// A text-only WebSocket connection. Abstracted so the signaling client is
/// testable without a network.
public protocol WebSocketTransport: Sendable {
    func send(_ text: String) async throws
    func receive() async throws -> String
    func sendPing() async throws
    func close()
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
                throw response.statusCode == 401
                    ? WebSocketTransportError.unauthorized
                    : WebSocketTransportError.httpStatus(response.statusCode)
            }
            throw error
        }
        return transport
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

    func receive() async throws -> String {
        switch try await task.receive() {
        case .string(let text):
            return text
        case .data:
            throw WebSocketTransportError.unexpectedBinaryMessage
        @unknown default:
            throw WebSocketTransportError.unexpectedBinaryMessage
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
}
