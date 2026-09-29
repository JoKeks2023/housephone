import Foundation

// Messages between the iPhone app and the Watch app over WatchConnectivity.
// WatchConnectivity carries property-list dictionaries; these types convert
// to and from them so both apps agree on the keys and the logic stays
// testable without WatchConnectivity.

/// iPhone → Watch: pair with this bridge using a companion pairing code.
public struct CompanionPairingInstruction: Equatable, Sendable {
    public var link: PairingLink
    public var expiresAt: Date

    public init(link: PairingLink, expiresAt: Date) {
        self.link = link
        self.expiresAt = expiresAt
    }

    public init(pairing: CompanionPairing, bridgeName: String?) {
        self.init(link: PairingLink(bridgeURL: pairing.url, code: pairing.code, bridgeName: bridgeName), expiresAt: pairing.expiresAt)
    }

    public static let messageType = "housephone.pair"

    public var dictionary: [String: Any] {
        [
            "type": Self.messageType,
            "url": link.bridgeURL.absoluteString,
            "code": link.code,
            "bridgeName": link.bridgeName ?? "",
            "expiresAt": expiresAt.timeIntervalSince1970,
        ]
    }

    public init?(dictionary: [String: Any]) {
        guard dictionary["type"] as? String == Self.messageType,
              let urlString = dictionary["url"] as? String,
              let url = URL(string: urlString),
              let rawCode = dictionary["code"] as? String,
              let code = PairingCode.normalize(rawCode),
              let expires = dictionary["expiresAt"] as? Double
        else { return nil }
        let name = (dictionary["bridgeName"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        self.init(link: PairingLink(bridgeURL: url, code: code, bridgeName: name), expiresAt: Date(timeIntervalSince1970: expires))
    }

    public func isExpired(at now: Date = .now) -> Bool { now >= expiresAt }
}

/// iPhone → Watch: the iPhone was unpaired, so the watch unpairs as well.
///
/// It may travel through the WatchConnectivity queue and arrive much later.
/// `applies(toPairingAt:)` keeps such a late instruction from undoing a
/// pairing the watch made after the iPhone unpaired.
public struct CompanionUnpairInstruction: Equatable, Sendable {
    public var issuedAt: Date

    public init(issuedAt: Date = .now) {
        self.issuedAt = issuedAt
    }

    public static let messageType = "housephone.unpair"

    public var dictionary: [String: Any] {
        ["type": Self.messageType, "issuedAt": issuedAt.timeIntervalSince1970]
    }

    public init?(dictionary: [String: Any]) {
        guard dictionary["type"] as? String == Self.messageType,
              let issued = dictionary["issuedAt"] as? Double
        else { return nil }
        self.init(issuedAt: Date(timeIntervalSince1970: issued))
    }

    /// Whether this instruction is meant for a pairing made at `pairedAt`
    /// (`nil`: unknown, e.g. paired by an older version).
    public func applies(toPairingAt pairedAt: Date?) -> Bool {
        guard let pairedAt else { return true }
        return issuedAt >= pairedAt
    }
}

/// The `type` of a WatchConnectivity dictionary, e.g. to find queued
/// transfers of one kind.
public func companionMessageType(of dictionary: [String: Any]) -> String? {
    dictionary["type"] as? String
}

/// Watch → iPhone: what the watch knows about its own pairing. Sent as the
/// reply to a pairing instruction and kept current in the application
/// context, so the iPhone shows the right state even after a restart.
public struct WatchPairingState: Codable, Equatable, Sendable {
    public enum Phase: String, Codable, Sendable {
        case unpaired
        case pairing
        case paired
        case failed
    }

    public var phase: Phase
    public var bridgeName: String?
    /// Push token registered at the bridge, so incoming calls can ring.
    public var canReceiveCalls: Bool
    /// Why pairing failed, in the user's language.
    public var failure: String?
    public var updatedAt: Date

    public init(phase: Phase, bridgeName: String? = nil, canReceiveCalls: Bool = false, failure: String? = nil, updatedAt: Date = .now) {
        self.phase = phase
        self.bridgeName = bridgeName
        self.canReceiveCalls = canReceiveCalls
        self.failure = failure
        self.updatedAt = updatedAt
    }

    public static let key = "housephone.watchState"

    public var dictionary: [String: Any] {
        guard let data = try? JSONEncoder().encode(self) else { return [:] }
        return [Self.key: data]
    }

    public init?(dictionary: [String: Any]) {
        guard let data = dictionary[Self.key] as? Data,
              let state = try? JSONDecoder().decode(Self.self, from: data)
        else { return nil }
        self = state
    }
}
