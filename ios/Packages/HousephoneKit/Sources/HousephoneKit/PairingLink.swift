import Foundation

public enum PairingLinkError: Error, Equatable, Sendable {
    case notAPairingLink
    case missingBridgeURL
    case invalidBridgeURL
    case missingCode
    case invalidCode
}

/// One-time pairing codes: 10 characters from `A-Z2-9` without `0 O 1 I`.
public enum PairingCode {
    public static let alphabet = Set("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")
    public static let length = 10

    /// Uppercases the input and drops spaces and dashes, so codes typed by
    /// hand or copied with grouping still work. Returns `nil` if the
    /// result is not a valid code.
    public static func normalize(_ input: String) -> String? {
        let cleaned = input.uppercased().filter { !$0.isWhitespace && $0 != "-" }
        guard cleaned.count == length, cleaned.allSatisfy(alphabet.contains) else { return nil }
        return cleaned
    }
}

/// `housephone://pair?url=<wss-URL>&code=<code>&name=<bridge name>`
public struct PairingLink: Equatable, Sendable {
    public let bridgeURL: URL
    public let code: String
    public let bridgeName: String?

    public init(bridgeURL: URL, code: String, bridgeName: String?) {
        self.bridgeURL = bridgeURL
        self.code = code
        self.bridgeName = bridgeName
    }

    public init(url: URL) throws(PairingLinkError) {
        guard url.scheme?.lowercased() == "housephone",
              url.host()?.lowercased() == "pair",
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        else { throw .notAPairingLink }

        let items = components.queryItems ?? []
        func value(_ name: String) -> String? {
            items.first { $0.name == name }?.value?.trimmingCharacters(in: .whitespacesAndNewlines)
        }

        guard let rawBridgeURL = value("url"), !rawBridgeURL.isEmpty else { throw .missingBridgeURL }
        guard let bridgeURL = URL(string: rawBridgeURL),
              let scheme = bridgeURL.scheme?.lowercased(), scheme == "wss" || scheme == "ws",
              let host = bridgeURL.host(), !host.isEmpty
        else { throw .invalidBridgeURL }

        guard let rawCode = value("code"), !rawCode.isEmpty else { throw .missingCode }
        guard let code = PairingCode.normalize(rawCode) else { throw .invalidCode }

        let name = value("name")
        self.init(bridgeURL: bridgeURL, code: code, bridgeName: name?.isEmpty == false ? name : nil)
    }

    /// Parses pasted text, tolerating surrounding whitespace.
    public init(string: String) throws(PairingLinkError) {
        let trimmed = string.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed) else { throw .notAPairingLink }
        try self.init(url: url)
    }

    /// `ws://` is only acceptable for tests on the local network.
    public var isEncrypted: Bool { bridgeURL.scheme?.lowercased() == "wss" }
}
