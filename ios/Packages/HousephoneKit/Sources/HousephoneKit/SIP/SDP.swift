import Foundation

/// The subset of SDP (RFC 4566, offer/answer RFC 3264) a G.711 phone
/// needs: one audio stream, PCMA or PCMU, RFC 4733 telephone events.
public struct SDPMedia: Sendable, Equatable {
    public enum Codec: UInt8, Sendable {
        case pcmu = 0
        case pcma = 8

        var encodingName: String { self == .pcma ? "PCMA" : "PCMU" }
    }

    public enum Direction: String, Sendable {
        case sendrecv, sendonly, recvonly, inactive
    }

    /// Where the remote side wants RTP.
    public var address: String
    public var port: UInt16
    public var codec: Codec
    /// Payload type for `telephone-event/8000`, if offered.
    public var telephoneEventPayloadType: UInt8?
    public var direction: Direction
    public var ptime: Int

    public init(address: String, port: UInt16, codec: Codec = .pcma, telephoneEventPayloadType: UInt8? = 101, direction: Direction = .sendrecv, ptime: Int = 20) {
        self.address = address
        self.port = port
        self.codec = codec
        self.telephoneEventPayloadType = telephoneEventPayloadType
        self.direction = direction
        self.ptime = ptime
    }
}

public enum SDP {
    public enum NegotiationError: Error, Equatable {
        case notSDP
        case noAudio
        case noCommonCodec
        case missingAddress
    }

    /// Codecs this phone decodes, in preference order.
    public static let supportedCodecs: [SDPMedia.Codec] = [.pcma]

    /// An offer (or answer) for our side.
    /// `codecs` lists the payload types to put on the m-line.
    public static func make(
        address: String,
        port: UInt16,
        codecs: [SDPMedia.Codec] = supportedCodecs,
        telephoneEvent: UInt8? = 101,
        direction: SDPMedia.Direction = .sendrecv,
        sessionID: UInt64 = UInt64.random(in: 1...UInt64(Int64.max)),
        version: UInt64 = 1
    ) -> Data {
        let family = address.contains(":") ? "IP6" : "IP4"
        var payloadTypes = codecs.map { String($0.rawValue) }
        if let telephoneEvent { payloadTypes.append(String(telephoneEvent)) }
        var lines = [
            "v=0",
            "o=- \(sessionID) \(version) IN \(family) \(address)",
            "s=Housephone",
            "c=IN \(family) \(address)",
            "t=0 0",
            "m=audio \(port) RTP/AVP \(payloadTypes.joined(separator: " "))",
        ]
        for codec in codecs { lines.append("a=rtpmap:\(codec.rawValue) \(codec.encodingName)/8000") }
        if let telephoneEvent {
            lines.append("a=rtpmap:\(telephoneEvent) telephone-event/8000")
            lines.append("a=fmtp:\(telephoneEvent) 0-15")
        }
        lines.append("a=ptime:20")
        lines.append("a=\(direction.rawValue)")
        return Data((lines.joined(separator: "\r\n") + "\r\n").utf8)
    }

    /// Reads the remote audio stream and picks the first codec of ours the
    /// remote side lists (in the remote's order, as an answerer should).
    public static func negotiate(_ body: Data, supported: [SDPMedia.Codec] = supportedCodecs) throws(NegotiationError) -> SDPMedia {
        guard let text = String(data: body, encoding: .utf8), text.hasPrefix("v=") else { throw .notSDP }
        var sessionAddress: String?
        var mediaAddress: String?
        var port: UInt16?
        var payloadTypes: [UInt8] = []
        var rtpmap: [UInt8: String] = [:]
        var direction = SDPMedia.Direction.sendrecv
        var sessionDirection: SDPMedia.Direction?
        var ptime = 20
        var inAudio = false
        var sawAudio = false

        for rawLine in text.split(whereSeparator: \.isNewline) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard line.count >= 2, line.dropFirst().first == "=" else { continue }
            let value = String(line.dropFirst(2))
            switch line.first {
            case "m":
                // Only the first audio stream counts.
                if sawAudio { inAudio = false; continue }
                let parts = value.split(separator: " ")
                inAudio = parts.first == "audio"
                guard inAudio, parts.count >= 4, let mediaPort = UInt16(parts[1]) else { inAudio = false; continue }
                sawAudio = true
                port = mediaPort
                payloadTypes = parts.dropFirst(3).compactMap { UInt8($0) }
            case "c":
                let parts = value.split(separator: " ")
                guard parts.count >= 3 else { continue }
                let address = String(parts[2].split(separator: "/").first ?? parts[2])
                if inAudio { mediaAddress = address } else if !sawAudio { sessionAddress = address }
            case "a":
                if let direct = SDPMedia.Direction(rawValue: value) {
                    if inAudio { direction = direct } else if !sawAudio { sessionDirection = direct }
                } else if value.hasPrefix("rtpmap:"), inAudio {
                    let parts = value.dropFirst(7).split(separator: " ", maxSplits: 1)
                    if parts.count == 2, let type = UInt8(parts[0]) { rtpmap[type] = parts[1].lowercased() }
                } else if value.hasPrefix("ptime:"), inAudio, let value = Int(value.dropFirst(6)) {
                    ptime = value
                }
            default:
                continue
            }
        }

        guard let port else { throw .noAudio }
        guard let address = mediaAddress ?? sessionAddress else { throw .missingAddress }
        if let sessionDirection, direction == .sendrecv { direction = sessionDirection }

        func codec(for type: UInt8) -> SDPMedia.Codec? {
            if let name = rtpmap[type] {
                if name.hasPrefix("pcma/8000") { return .pcma }
                if name.hasPrefix("pcmu/8000") { return .pcmu }
                return nil
            }
            // Static payload types may come without rtpmap.
            return SDPMedia.Codec(rawValue: type)
        }
        guard let chosen = payloadTypes.lazy.compactMap(codec).first(where: supported.contains) else { throw .noCommonCodec }
        let telephoneEvent = payloadTypes.first { rtpmap[$0]?.hasPrefix("telephone-event/8000") == true }
        return SDPMedia(address: address, port: port, codec: chosen, telephoneEventPayloadType: telephoneEvent, direction: direction, ptime: ptime)
    }
}
