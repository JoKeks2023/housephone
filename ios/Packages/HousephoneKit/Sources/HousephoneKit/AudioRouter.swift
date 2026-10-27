import Foundation

/// Hands binary audio messages to whoever plays them right now. Thread
/// safe: the network delivers on a background task. The signaling client's
/// audio stream has a single reader for its whole life; calls set a handler
/// while their audio runs over the WebSocket.
public final class AudioRouter: @unchecked Sendable {
    private let lock = NSLock()
    private var handler: (@Sendable (Data) -> Void)?

    public init() {}

    public func setHandler(_ handler: (@Sendable (Data) -> Void)?) {
        lock.withLock { self.handler = handler }
    }

    public func deliver(_ message: Data) {
        let handler = lock.withLock { self.handler }
        handler?(message)
    }
}
