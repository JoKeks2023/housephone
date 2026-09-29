import Foundation

/// An RTP packet (RFC 3550 §5.1). Parsing skips CSRCs, header extensions
/// and padding; serializing writes the plain 12-byte header.
public struct RTPPacket: Sendable, Equatable {
    public var payloadType: UInt8
    public var marker: Bool
    public var sequenceNumber: UInt16
    public var timestamp: UInt32
    public var ssrc: UInt32
    public var payload: Data

    public init(payloadType: UInt8, marker: Bool = false, sequenceNumber: UInt16, timestamp: UInt32, ssrc: UInt32, payload: Data) {
        self.payloadType = payloadType
        self.marker = marker
        self.sequenceNumber = sequenceNumber
        self.timestamp = timestamp
        self.ssrc = ssrc
        self.payload = payload
    }

    public init?(parsing data: Data) {
        let bytes = [UInt8](data)
        guard bytes.count >= 12, bytes[0] >> 6 == 2 else { return nil }
        let hasPadding = bytes[0] & 0x20 != 0
        let hasExtension = bytes[0] & 0x10 != 0
        let csrcCount = Int(bytes[0] & 0x0F)
        marker = bytes[1] & 0x80 != 0
        payloadType = bytes[1] & 0x7F
        sequenceNumber = UInt16(bytes[2]) << 8 | UInt16(bytes[3])
        timestamp = bytes[4...7].reduce(0) { $0 << 8 | UInt32($1) }
        ssrc = bytes[8...11].reduce(0) { $0 << 8 | UInt32($1) }

        var start = 12 + csrcCount * 4
        if hasExtension {
            guard bytes.count >= start + 4 else { return nil }
            let words = Int(bytes[start + 2]) << 8 | Int(bytes[start + 3])
            start += 4 + words * 4
        }
        var end = bytes.count
        if hasPadding {
            guard let count = bytes.last, count > 0 else { return nil }
            end -= Int(count)
        }
        guard start <= end else { return nil }
        payload = Data(bytes[start..<end])
    }

    public func serialized() -> Data {
        var data = Data(capacity: 12 + payload.count)
        data.append(0x80)
        data.append((marker ? 0x80 : 0) | (payloadType & 0x7F))
        data.append(UInt8(sequenceNumber >> 8))
        data.append(UInt8(sequenceNumber & 0xFF))
        for shift in stride(from: 24, through: 0, by: -8) { data.append(UInt8((timestamp >> UInt32(shift)) & 0xFF)) }
        for shift in stride(from: 24, through: 0, by: -8) { data.append(UInt8((ssrc >> UInt32(shift)) & 0xFF)) }
        data.append(payload)
        return data
    }
}

/// Numbers outgoing packets: one SSRC, running sequence numbers,
/// timestamps in 8-kHz samples.
public struct RTPSender: Sendable {
    public let ssrc: UInt32
    public var payloadType: UInt8
    public var telephoneEventPayloadType: UInt8?
    private var sequenceNumber: UInt16
    private var timestamp: UInt32
    private var isFirst = true

    public init(payloadType: UInt8, telephoneEventPayloadType: UInt8? = nil, ssrc: UInt32 = .random(in: .min ... .max), sequenceNumber: UInt16 = .random(in: .min ... .max), timestamp: UInt32 = .random(in: .min ... .max)) {
        self.payloadType = payloadType
        self.telephoneEventPayloadType = telephoneEventPayloadType
        self.ssrc = ssrc
        self.sequenceNumber = sequenceNumber
        self.timestamp = timestamp
    }

    /// One 20-ms audio frame (160 samples).
    public mutating func audio(_ payload: Data) -> Data {
        let packet = RTPPacket(payloadType: payloadType, marker: isFirst, sequenceNumber: sequenceNumber, timestamp: timestamp, ssrc: ssrc, payload: payload)
        isFirst = false
        sequenceNumber &+= 1
        timestamp &+= UInt32(payload.count)
        return packet.serialized()
    }

    /// RFC 4733 event packets for one DTMF digit, to be sent 20 ms apart
    /// in place of audio: growing duration, then the end packet three
    /// times. Nil without a negotiated telephone-event payload type.
    public mutating func dtmf(_ digit: Character, milliseconds: Int = 120) -> [Data]? {
        guard let telephoneEventPayloadType, let event = Self.event(for: digit) else { return nil }
        let steps = max(milliseconds / 20, 1)
        let start = timestamp
        var packets: [Data] = []
        for step in 1...steps {
            let isEnd = step == steps
            let duration = UInt16(clamping: step * 160)
            for _ in 0..<(isEnd ? 3 : 1) {
                let payload = Data([event, (isEnd ? 0x80 : 0) | 10, UInt8(duration >> 8), UInt8(duration & 0xFF)])
                packets.append(RTPPacket(payloadType: telephoneEventPayloadType, marker: step == 1, sequenceNumber: sequenceNumber, timestamp: start, ssrc: ssrc, payload: payload).serialized())
                sequenceNumber &+= 1
            }
        }
        timestamp &+= UInt32(steps * 160)
        return packets
    }

    static func event(for digit: Character) -> UInt8? {
        switch digit {
        case "0"..."9": UInt8(String(digit))
        case "*": 10
        case "#": 11
        case "A", "a": 12
        case "B", "b": 13
        case "C", "c": 14
        case "D", "d": 15
        default: nil
        }
    }
}
