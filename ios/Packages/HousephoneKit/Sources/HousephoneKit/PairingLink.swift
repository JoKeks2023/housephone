import Foundation

public enum PairingLinkError: Error, Equatable, Sendable {
    case notAPairingLink
    case missingBridgeURL
    case invalidBridgeURL
    case missingCode
    case invalidCode
    /// A link from a bridge before signaling v2 (no `v=2`).
    case outdatedLink
    /// No or no valid bridge fingerprint (`fp`).
    case invalidFingerprint
}

/// One-time pairing codes: 16 characters from `A-Z2-9` without `0 O 1 I`
/// (80 bits), shown as `XXXX-XXXX-XXXX-XXXX`.
public enum PairingCode {
    public static let alphabet = Set("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")
    public static let length = 16

    /// Uppercases the input and drops spaces and dashes, so codes typed by
    /// hand or copied with grouping still work. Returns `nil` if the
    /// result is not a valid code.
    public static func normalize(_ input: String) -> String? {
        let cleaned = input.uppercased().filter { !$0.isWhitespace && $0 != "-" }
        guard cleaned.count == length, cleaned.allSatisfy(alphabet.contains) else { return nil }
        return cleaned
    }

    /// `K7P2XH9QRMW4DZT8` → `K7P2-XH9Q-RMW4-DZT8`.
    public static func grouped(_ code: String) -> String {
        stride(from: 0, to: code.count, by: 4).map { offset in
            let start = code.index(code.startIndex, offsetBy: offset)
            let end = code.index(start, offsetBy: 4, limitedBy: code.endIndex) ?? code.endIndex
            return String(code[start..<end])
        }.joined(separator: "-")
    }
}

/// `housephone://pair?v=2&url=<wss-URL>&code=<code>&fp=<fingerprint>&name=<bridge name>`
///
/// `fp` is `base64url(SHA-256(bridge key))`. Pairing succeeds only if the
/// bridge proves it holds exactly that key, so a link read from the
/// bridge's own screen pins the right bridge.
public struct PairingLink: Equatable, Sendable {
    public let bridgeURL: URL
    public let code: String
    public let bridgeName: String?
    public let fingerprint: String

    public init(bridgeURL: URL, code: String, bridgeName: String?, fingerprint: String) {
        self.bridgeURL = bridgeURL
        self.code = code
        self.bridgeName = bridgeName
        self.fingerprint = fingerprint
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

        guard value("v") == "2" else { throw .outdatedLink }

        guard let rawBridgeURL = value("url"), !rawBridgeURL.isEmpty else { throw .missingBridgeURL }
        guard let bridgeURL = URL(string: rawBridgeURL),
              let scheme = bridgeURL.scheme?.lowercased(), scheme == "wss" || scheme == "ws",
              let host = bridgeURL.host(), !host.isEmpty
        else { throw .invalidBridgeURL }

        guard let rawCode = value("code"), !rawCode.isEmpty else { throw .missingCode }
        guard let code = PairingCode.normalize(rawCode) else { throw .invalidCode }

        guard let fingerprint = value("fp"), Self.isValidFingerprint(fingerprint) else { throw .invalidFingerprint }

        let name = value("name")
        self.init(bridgeURL: bridgeURL, code: code, bridgeName: name?.isEmpty == false ? name : nil, fingerprint: fingerprint)
    }

    /// Parses pasted text, tolerating surrounding whitespace.
    public init(string: String) throws(PairingLinkError) {
        let trimmed = string.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed) else { throw .notAPairingLink }
        try self.init(url: url)
    }

    /// `ws://` is only acceptable for tests on the local network.
    public var isEncrypted: Bool { bridgeURL.scheme?.lowercased() == "wss" }

    /// The code as shown on the server, `XXXX-XXXX-XXXX-XXXX`.
    public var groupedCode: String { PairingCode.grouped(code) }

    /// A SHA-256 digest in base64url: 43 characters, 32 bytes.
    public static func isValidFingerprint(_ value: String) -> Bool {
        value.count == 43 && HP2.data(base64URL: value)?.count == 32
    }
}
