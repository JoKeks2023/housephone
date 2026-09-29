import Foundation

/// Generates the ringback tone ("Freiton") the app plays while the remote
/// party rings and the network sends no early media.
public enum RingbackTone {
    /// German ringback: 425 Hz, 1 s tone, 4 s silence. One cycle, meant to loop.
    public static func germanWAV(sampleRate: Int = 16_000) -> Data {
        wav(frequency: 425, toneDuration: 1, silenceDuration: 4, sampleRate: sampleRate, amplitude: 0.2)
    }

    /// 16-bit PCM, mono, little endian. The tone fades in and out over 10 ms
    /// so it doesn't click.
    public static func wav(frequency: Double, toneDuration: Double, silenceDuration: Double, sampleRate: Int, amplitude: Double) -> Data {
        let toneSamples = Int(toneDuration * Double(sampleRate))
        let totalSamples = toneSamples + Int(silenceDuration * Double(sampleRate))
        let fadeSamples = max(1, sampleRate / 100)
        let peak = Double(Int16.max) * min(max(amplitude, 0), 1)

        var pcm = Data(capacity: totalSamples * 2)
        for index in 0..<totalSamples {
            var sample = 0.0
            if index < toneSamples {
                let envelope = min(1, Double(min(index, toneSamples - 1 - index)) / Double(fadeSamples))
                sample = sin(2 * .pi * frequency * Double(index) / Double(sampleRate)) * peak * envelope
            }
            let value = Int16(sample.rounded())
            pcm.append(UInt8(truncatingIfNeeded: value))
            pcm.append(UInt8(truncatingIfNeeded: value >> 8))
        }

        var data = Data()
        data.append(contentsOf: Array("RIFF".utf8))
        data.appendLittleEndian(UInt32(36 + pcm.count))
        data.append(contentsOf: Array("WAVE".utf8))
        data.append(contentsOf: Array("fmt ".utf8))
        data.appendLittleEndian(UInt32(16)) // PCM chunk size
        data.appendLittleEndian(UInt16(1)) // PCM format
        data.appendLittleEndian(UInt16(1)) // mono
        data.appendLittleEndian(UInt32(sampleRate))
        data.appendLittleEndian(UInt32(sampleRate * 2)) // byte rate
        data.appendLittleEndian(UInt16(2)) // block align
        data.appendLittleEndian(UInt16(16)) // bits per sample
        data.append(contentsOf: Array("data".utf8))
        data.appendLittleEndian(UInt32(pcm.count))
        data.append(pcm)
        return data
    }
}

private extension Data {
    mutating func appendLittleEndian<T: FixedWidthInteger>(_ value: T) {
        Swift.withUnsafeBytes(of: value.littleEndian) { append(contentsOf: $0) }
    }
}
