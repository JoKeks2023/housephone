import Foundation

/// ITU-T G.711 A-law, the codec of the `websocket-pcma` media path.
///
/// Follows the reference algorithm (Sun Microsystems' public-domain
/// `g711.c`): 13-bit magnitude, 8 segments, even bits inverted (XOR 0x55).
/// Digital silence encodes as `0xD5`.
public enum G711 {
    /// The A-law code for digital silence.
    public static let aLawSilence: UInt8 = 0xD5

    private static let segmentEnds: [Int] = [0x1F, 0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF]

    /// Encodes one 16-bit linear sample.
    public static func aLaw(fromLinear sample: Int16) -> UInt8 {
        var value = Int(sample) >> 3
        let mask: Int
        if value >= 0 {
            mask = 0xD5
        } else {
            mask = 0x55
            value = -value - 1
        }

        guard let segment = segmentEnds.firstIndex(where: { value <= $0 }) else {
            // Out of range: clip to the largest magnitude.
            return UInt8(0x7F ^ mask)
        }
        var code = segment << 4
        code |= segment < 2 ? (value >> 1) & 0x0F : (value >> segment) & 0x0F
        return UInt8(code ^ mask)
    }

    /// Decodes one A-law code to a 16-bit linear sample.
    public static func linear(fromALaw code: UInt8) -> Int16 {
        decodeTable[Int(code)]
    }

    private static let decodeTable: [Int16] = (0...255).map { code in
        let value = Int(code) ^ 0x55
        var magnitude = (value & 0x0F) << 4
        let segment = (value & 0x70) >> 4
        switch segment {
        case 0: magnitude += 8
        case 1: magnitude += 0x108
        default:
            magnitude += 0x108
            magnitude <<= segment - 1
        }
        return Int16(value & 0x80 != 0 ? magnitude : -magnitude)
    }

    public static func encodeALaw(_ samples: some Collection<Int16>) -> Data {
        Data(samples.map(aLaw(fromLinear:)))
    }

    public static func decodeALaw(_ codes: some Collection<UInt8>) -> [Int16] {
        codes.map(linear(fromALaw:))
    }
}
