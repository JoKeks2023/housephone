import CryptoKit
import Foundation

/// HTTP Digest authentication as SIP uses it (RFC 3261 §22.4, RFC 2617):
/// MD5, with or without `qop=auth`.
public struct SIPDigestChallenge: Sendable, Equatable {
    public var realm: String
    public var nonce: String
    public var opaque: String?
    public var algorithm: String?
    /// The offered qop options, e.g. `["auth", "auth-int"]`.
    public var qop: [String]
    public var stale: Bool

    /// Parses a `WWW-Authenticate` / `Proxy-Authenticate` value.
    public init?(header: String) {
        let trimmed = header.trimmingCharacters(in: .whitespaces)
        guard trimmed.lowercased().hasPrefix("digest") else { return nil }
        let parameters = Self.parameters(String(trimmed.dropFirst(6)))
        guard let realm = parameters["realm"], let nonce = parameters["nonce"] else { return nil }
        self.realm = realm
        self.nonce = nonce
        opaque = parameters["opaque"]
        algorithm = parameters["algorithm"]
        qop = parameters["qop"].map { $0.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces).lowercased() } } ?? []
        stale = parameters["stale"]?.lowercased() == "true"
    }

    /// `key=value` / `key="value"` pairs separated by commas.
    static func parameters(_ text: String) -> [String: String] {
        var result: [String: String] = [:]
        var index = text.startIndex
        func skip(_ characters: Set<Character>) {
            while index < text.endIndex, characters.contains(text[index]) { index = text.index(after: index) }
        }
        while index < text.endIndex {
            skip([" ", "\t", ","])
            let keyStart = index
            while index < text.endIndex, text[index] != "=", text[index] != "," { index = text.index(after: index) }
            let key = text[keyStart..<index].trimmingCharacters(in: .whitespaces).lowercased()
            guard index < text.endIndex, text[index] == "=" else { continue }
            index = text.index(after: index)
            skip([" ", "\t"])
            var value = ""
            if index < text.endIndex, text[index] == "\"" {
                index = text.index(after: index)
                while index < text.endIndex, text[index] != "\"" {
                    if text[index] == "\\", text.index(after: index) < text.endIndex { index = text.index(after: index) }
                    value.append(text[index])
                    index = text.index(after: index)
                }
                if index < text.endIndex { index = text.index(after: index) }
            } else {
                let valueStart = index
                while index < text.endIndex, text[index] != "," { index = text.index(after: index) }
                value = text[valueStart..<index].trimmingCharacters(in: .whitespaces)
            }
            if !key.isEmpty { result[key] = value }
        }
        return result
    }

    public var supportsMD5: Bool {
        algorithm == nil || algorithm?.uppercased() == "MD5"
    }
}

public enum SIPDigest {
    /// The `Authorization` / `Proxy-Authorization` header value.
    /// `cnonce` and `nonceCount` are parameters for tests.
    public static func authorization(
        challenge: SIPDigestChallenge,
        method: String,
        uri: String,
        username: String,
        password: String,
        cnonce: String = randomToken(16),
        nonceCount: UInt32 = 1
    ) -> String {
        let ha1 = md5Hex("\(username):\(challenge.realm):\(password)")
        let ha2 = md5Hex("\(method):\(uri)")
        let useQop = challenge.qop.contains("auth")
        let nc = String(format: "%08x", nonceCount)
        let response = useQop
            ? md5Hex("\(ha1):\(challenge.nonce):\(nc):\(cnonce):auth:\(ha2)")
            : md5Hex("\(ha1):\(challenge.nonce):\(ha2)")

        var parts = [
            "username=\"\(username)\"",
            "realm=\"\(challenge.realm)\"",
            "nonce=\"\(challenge.nonce)\"",
            "uri=\"\(uri)\"",
            "response=\"\(response)\"",
            "algorithm=MD5",
        ]
        if let opaque = challenge.opaque { parts.append("opaque=\"\(opaque)\"") }
        if useQop { parts += ["qop=auth", "nc=\(nc)", "cnonce=\"\(cnonce)\""] }
        return "Digest " + parts.joined(separator: ", ")
    }

    static func md5Hex(_ text: String) -> String {
        Insecure.MD5.hash(data: Data(text.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    /// Random lowercase hex, for tags, branches, Call-IDs and cnonces.
    public static func randomToken(_ bytes: Int = 8) -> String {
        (0..<bytes).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
    }
}
