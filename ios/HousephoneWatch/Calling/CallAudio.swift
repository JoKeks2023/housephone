import AVFAudio
import Foundation
import HousephoneKit
import os

/// Audio of the watch's `websocket-pcma` calls.
///
/// - Microphone → voice processing (echo cancellation) → 8 kHz Int16 →
///   A-law → 160-byte frames → `onFrame`.
/// - Network frames → jitter buffer → A-law decode → 8 kHz buffers on an
///   `AVAudioPlayerNode`. The player's own completion callbacks pull the
///   next frame, so playout runs on the audio hardware clock.
///
/// Runs only between CallKit's `didActivate` and `didDeactivate`. Thread
/// safe: CallKit, the network and the audio threads call in concurrently.
final class CallAudio: @unchecked Sendable {
    /// Called on an audio thread with each outgoing audio message.
    var onFrame: (@Sendable (Data) -> Void)? {
        get { lock.withLock { frameHandler } }
        set { lock.withLock { frameHandler = newValue } }
    }

    private let lock = NSLock()
    private let logger = Logger(subsystem: "com.jorisconrad.housephone.watch", category: "audio")

    // Guarded by `lock`.
    private var frameHandler: (@Sendable (Data) -> Void)?
    private var engine: AVAudioEngine?
    private var player: AVAudioPlayerNode?
    private var capture: CaptureConverter?
    private var jitter = JitterBuffer()
    private var isRunning = false
    private var isMuted = false
    private var isRingbackOn = false
    private var ringback = RingbackGenerator()
    private var volume: Float = 1

    private static let playbackFormat = AVAudioFormat(
        commonFormat: .pcmFormatFloat32,
        sampleRate: Double(AudioFrame.sampleRate),
        channels: 1,
        interleaved: false
    )!
    /// Buffers kept scheduled ahead on the player (3 × 20 ms).
    private static let scheduledAhead = 3

    // MARK: - Session

    /// Sets the category before CallKit activates the session.
    static func configureSession() {
        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [])
        } catch {
            Logger(subsystem: "com.jorisconrad.housephone.watch", category: "audio")
                .error("Audio session category failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Lifecycle

    /// Call from `provider(_:didActivate:)`.
    func start() {
        lock.lock()
        defer { lock.unlock() }
        guard !isRunning else { return }

        let engine = AVAudioEngine()
        let input = engine.inputNode
        do {
            try input.setVoiceProcessingEnabled(true)
        } catch {
            logger.error("Voice processing unavailable: \(error.localizedDescription, privacy: .public)")
        }

        let player = AVAudioPlayerNode()
        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: Self.playbackFormat)
        player.volume = volume

        let inputFormat = input.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, let capture = CaptureConverter(inputFormat: inputFormat) else {
            logger.error("No usable microphone format: \(inputFormat.description, privacy: .public)")
            return
        }
        input.installTap(onBus: 0, bufferSize: 1024, format: inputFormat) { [weak self] buffer, _ in
            self?.captured(buffer)
        }

        do {
            engine.prepare()
            try engine.start()
        } catch {
            input.removeTap(onBus: 0)
            logger.error("Audio engine did not start: \(error.localizedDescription, privacy: .public)")
            return
        }

        self.engine = engine
        self.player = player
        self.capture = capture
        isRunning = true
        for _ in 0..<Self.scheduledAhead { scheduleNextBufferLocked() }
        player.play()
        logger.info("Audio running, microphone \(inputFormat.sampleRate, privacy: .public) Hz")
    }

    /// Call from `provider(_:didDeactivate:)` and when the call ends.
    func stop() {
        lock.lock()
        isRunning = false
        let engine = self.engine
        let player = self.player
        self.engine = nil
        self.player = nil
        capture = nil
        jitter.reset()
        lock.unlock()

        // Outside the lock: stopping flushes buffers, whose completion
        // callbacks take the lock.
        player?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        engine?.stop()
    }

    // MARK: - Controls

    func setMuted(_ muted: Bool) {
        lock.withLock { isMuted = muted }
    }

    func setRingback(_ on: Bool) {
        lock.withLock {
            if on, !isRingbackOn { ringback = RingbackGenerator() }
            isRingbackOn = on
        }
    }

    /// 0…1, from the Digital Crown.
    func setVolume(_ newValue: Float) {
        lock.withLock {
            volume = max(0, min(1, newValue))
            player?.volume = volume
        }
    }

    /// New media session (e.g. after a re-attach): drop stale audio.
    func resetPlayout() {
        lock.withLock { jitter.reset() }
    }

    // MARK: - Network → speaker

    /// One binary WebSocket message from the bridge.
    func receive(_ message: Data) {
        guard let payload = AudioFrame.decode(message) else { return }
        lock.withLock { jitter.push(payload) }
    }

    /// Must hold `lock`.
    private func scheduleNextBufferLocked() {
        guard isRunning, let player,
              let buffer = AVAudioPCMBuffer(pcmFormat: Self.playbackFormat, frameCapacity: AVAudioFrameCount(AudioFrame.samplesPerFrame)),
              let channel = buffer.floatChannelData?[0]
        else { return }
        buffer.frameLength = buffer.frameCapacity

        if isRingbackOn {
            ringback.fill(channel, count: AudioFrame.samplesPerFrame)
        } else {
            switch jitter.pull() {
            case .frame(let payload):
                Self.decode(payload, into: channel, gain: 1)
            case .concealment(let payload):
                Self.decode(payload, into: channel, gain: 0.5)
            case .silence:
                channel.update(repeating: 0, count: AudioFrame.samplesPerFrame)
            }
        }

        player.scheduleBuffer(buffer, completionCallbackType: .dataConsumed) { [weak self] _ in
            self?.bufferConsumed()
        }
    }

    private func bufferConsumed() {
        lock.withLock { scheduleNextBufferLocked() }
    }

    private static func decode(_ payload: Data, into channel: UnsafeMutablePointer<Float>, gain: Float) {
        var index = 0
        for code in payload.prefix(AudioFrame.samplesPerFrame) {
            channel[index] = Float(G711.linear(fromALaw: code)) / 32768 * gain
            index += 1
        }
        while index < AudioFrame.samplesPerFrame {
            channel[index] = 0
            index += 1
        }
    }

    // MARK: - Microphone → network

    private func captured(_ buffer: AVAudioPCMBuffer) {
        lock.lock()
        guard isRunning, let capture else {
            lock.unlock()
            return
        }
        let muted = isMuted
        let handler = frameHandler
        let frames = capture.convert(buffer)
        lock.unlock()

        guard let handler else { return }
        for samples in frames {
            let payload = muted ? AudioFrame.silence : G711.encodeALaw(samples)
            if let message = AudioFrame.encode(aLaw: payload) { handler(message) }
        }
    }
}

/// Converts microphone buffers to 8-kHz Int16 mono and cuts them into
/// 20-ms frames. Used only on the audio tap thread.
private final class CaptureConverter {
    private let converter: AVAudioConverter
    private let outputFormat: AVAudioFormat
    private var chunker = FrameChunker()

    init?(inputFormat: AVAudioFormat) {
        guard let outputFormat = AVAudioFormat(
            commonFormat: .pcmFormatInt16,
            sampleRate: Double(AudioFrame.sampleRate),
            channels: 1,
            interleaved: false
        ), let converter = AVAudioConverter(from: inputFormat, to: outputFormat) else { return nil }
        self.converter = converter
        self.outputFormat = outputFormat
    }

    func convert(_ buffer: AVAudioPCMBuffer) -> [[Int16]] {
        let ratio = outputFormat.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up)) + 32
        guard let output = AVAudioPCMBuffer(pcmFormat: outputFormat, frameCapacity: capacity) else { return [] }

        var consumed = false
        var error: NSError?
        let status = converter.convert(to: output, error: &error) { _, inputStatus in
            if consumed {
                inputStatus.pointee = .noDataNow
                return nil
            }
            consumed = true
            inputStatus.pointee = .haveData
            return buffer
        }
        guard status != .error, let samples = output.int16ChannelData?[0] else { return [] }
        return chunker.append(UnsafeBufferPointer(start: samples, count: Int(output.frameLength)))
    }
}

/// German ringback ("Freiton"): 425 Hz, 1 s on, 4 s off, at 8 kHz.
private struct RingbackGenerator {
    private var sampleIndex = 0
    private static let cycle = AudioFrame.sampleRate * 5
    private static let toneSamples = AudioFrame.sampleRate
    private static let step = 2 * Float.pi * 425 / Float(AudioFrame.sampleRate)

    mutating func fill(_ channel: UnsafeMutablePointer<Float>, count: Int) {
        for index in 0..<count {
            let position = sampleIndex % Self.cycle
            if position < Self.toneSamples {
                // 10-ms fade in and out so the tone doesn't click.
                let fade = Float(min(position, Self.toneSamples - 1 - position)) / 80
                channel[index] = sin(Float(position) * Self.step) * 0.2 * min(1, fade)
            } else {
                channel[index] = 0
            }
            sampleIndex += 1
        }
    }
}
