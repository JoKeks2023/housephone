import Foundation

/// Binary WebSocket message of the `websocket-pcma` media path
/// (signaling v1.1): one type byte, then 20 ms of A-law at 8 kHz.
public enum AudioFrame {
    public static let audioType: UInt8 = 0x01
    public static let sampleRate = 8000
    public static let samplesPerFrame = 160
    public static let duration: Duration = .milliseconds(20)

    /// Wraps exactly 160 A-law bytes. Returns `nil` for any other length.
    public static func encode(aLaw payload: Data) -> Data? {
        guard payload.count == samplesPerFrame else { return nil }
        var message = Data(capacity: samplesPerFrame + 1)
        message.append(audioType)
        message.append(payload)
        return message
    }

    /// The A-law payload of an audio message, or `nil` for reserved types
    /// and malformed messages (which the spec says to ignore).
    public static func decode(_ message: Data) -> Data? {
        guard message.count == samplesPerFrame + 1, message.first == audioType else { return nil }
        return Data(message.dropFirst())
    }

    /// 20 ms of digital silence.
    public static let silence = Data(repeating: G711.aLawSilence, count: samplesPerFrame)
}

/// Cuts a stream of 8 kHz samples, arriving in arbitrary chunk sizes from
/// the microphone, into 160-sample frames.
public struct FrameChunker: Sendable {
    public let frameSize: Int
    private var pending: [Int16] = []

    public init(frameSize: Int = AudioFrame.samplesPerFrame) {
        self.frameSize = frameSize
        pending.reserveCapacity(frameSize * 2)
    }

    /// Appends samples and returns every complete frame.
    public mutating func append(_ samples: some Collection<Int16>) -> [[Int16]] {
        pending.append(contentsOf: samples)
        var frames: [[Int16]] = []
        while pending.count >= frameSize {
            frames.append(Array(pending.prefix(frameSize)))
            pending.removeFirst(frameSize)
        }
        return frames
    }

    public mutating func reset() {
        pending.removeAll(keepingCapacity: true)
    }

    public var bufferedSampleCount: Int { pending.count }
}
