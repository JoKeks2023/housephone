import CryptoKit
import Foundation

// Pairing and authentication v2 (ADR-0004, signaling v2): device keys
// sign requests, the pinned bridge key signs responses, and session keys
// from an ephemeral X25519 exchange seal every WebSocket frame and HTTPS
// response end to end. Test vectors: docs/protocol/fixtures/crypto.

public enum HP2Error: Error, Equatable, Sendable {
    /// The response carries no `HP2-Bridge` header.
    case missingBridgeSignature
    /// The `HP2-Bridge` header is malformed or its signature is wrong.
    case invalidBridgeSignature
    /// Pairing: the bridge's key doesn't match the fingerprint in the QR
    /// code, or its signature over the pairing is wrong.
    case bridgeIdentityMismatch
    /// A sealed frame or body failed authentication, arrived out of order,
    /// or was not binary.
    case invalidFrame
    /// Malformed key material.
    case invalidKey
}

/// Kind of a decrypted WebSocket frame (the first plaintext byte).
public enum HP2FrameType: UInt8, Sendable {
    /// A signaling message as in v1: `{"type": …, "payload": …}`.
    case json = 0x00
    /// 160 bytes of A-law (`websocket-pcma`), same layout as `AudioFrame`.
    case audio = 0x01
}

/// Session keys derived per request or connection.
public struct HP2SessionKeys: Sendable {
    public let deviceToBridge: SymmetricKey
    public let bridgeToDevice: SymmetricKey
}

public enum HP2 {
    public static let authorizationScheme = "HP2"
    public static let bridgeHeaderName = "HP2-Bridge"
    public static let sealedContentType = "application/vnd.housephone.sealed"
    public static let nonceLength = 16
    static let aad = Data("HP2".utf8)

    // MARK: - Encoding

    public static func base64URL(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    /// Decodes base64url without padding. Rejects padding and the standard
    /// alphabet, so every value has exactly one accepted spelling.
    public static func data(base64URL string: String) -> Data? {
        guard !string.isEmpty,
              string.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") })
        else { return nil }
        var base64 = string.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while base64.count % 4 != 0 { base64 += "=" }
        return Data(base64Encoded: base64)
    }

    public static func sha256Hex(_ data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    /// `base64url(SHA-256(bridgePublicKey))`, as in the pairing link.
    public static func fingerprint(of bridgePublicKey: Data) -> String {
        base64URL(Data(SHA256.hash(data: bridgePublicKey)))
    }

    // MARK: - Signed messages (lines joined with "\n")

    static func lines(_ parts: String...) -> Data {
        Data(parts.joined(separator: "\n").utf8)
    }

    /// What the device signs to prove it holds the key it registers.
    public static func pairProofMessage(code: String, nonce: String, publicKey: String) -> Data {
        lines("HP2-PAIR-PROOF", code, nonce, publicKey)
    }

    /// What the bridge signs in its pairing response.
    public static func pairResponseMessage(bridgeId: String, deviceId: String, publicKey: String, nonce: String, code: String) -> Data {
        lines("HP2-PAIR", bridgeId, deviceId, publicKey, nonce, code)
    }

    /// What the device signs for every authenticated request.
    public static func authMessage(
        method: String,
        pathAndQuery: String,
        bridgeId: String,
        deviceId: String,
        timestamp: Int,
        nonce: String,
        ephemeralPublicKey: String,
        body: Data
    ) -> Data {
        lines("HP2-AUTH", method.uppercased(), pathAndQuery, bridgeId, deviceId, String(timestamp), nonce, ephemeralPublicKey, sha256Hex(body))
    }

    /// What the bridge signs for every response (including `101`).
    public static func bridgeMessage(
        bridgeId: String,
        deviceId: String,
        nonce: String,
        deviceEphemeralPublicKey: String,
        bridgeEphemeralPublicKey: String,
        status: Int,
        body: Data
    ) -> Data {
        lines("HP2-BRIDGE", bridgeId, deviceId, nonce, deviceEphemeralPublicKey, bridgeEphemeralPublicKey, String(status), sha256Hex(body))
    }

    // MARK: - Keys

    /// X25519, then HKDF-SHA256 (salt = request nonce) into one key per direction.
    public static func deriveKeys(
        devicePrivateKey: Curve25519.KeyAgreement.PrivateKey,
        bridgePublicKey: Curve25519.KeyAgreement.PublicKey,
        nonce: Data,
        bridgeId: String,
        deviceId: String
    ) throws -> HP2SessionKeys {
        let shared = try devicePrivateKey.sharedSecretFromKeyAgreement(with: bridgePublicKey)
        let info = lines(
            "HP2-KEYS", bridgeId, deviceId,
            base64URL(devicePrivateKey.publicKey.rawRepresentation),
            base64URL(bridgePublicKey.rawRepresentation)
        )
        let okm = shared.hkdfDerivedSymmetricKey(using: SHA256.self, salt: nonce, sharedInfo: info, outputByteCount: 64)
        let bytes = okm.withUnsafeBytes { Data($0) }
        return HP2SessionKeys(
            deviceToBridge: SymmetricKey(data: bytes.prefix(32)),
            bridgeToDevice: SymmetricKey(data: bytes.suffix(32))
        )
    }

    // MARK: - AEAD

    /// ChaCha20-Poly1305, nonce = 4 zero bytes ‖ counter (big endian),
    /// AAD `"HP2"`. Returns ciphertext ‖ tag.
    public static func seal(_ plaintext: Data, key: SymmetricKey, counter: UInt64) throws -> Data {
        let box = try ChaChaPoly.seal(plaintext, using: key, nonce: try nonce(counter), authenticating: aad)
        return box.ciphertext + box.tag
    }

    public static func open(_ sealed: Data, key: SymmetricKey, counter: UInt64) throws(HP2Error) -> Data {
        guard sealed.count >= 16 else { throw .invalidFrame }
        do {
            let box = try ChaChaPoly.SealedBox(nonce: try nonce(counter), ciphertext: sealed.dropLast(16), tag: sealed.suffix(16))
            return try ChaChaPoly.open(box, using: key, authenticating: aad)
        } catch {
            throw .invalidFrame
        }
    }

    private static func nonce(_ counter: UInt64) throws -> ChaChaPoly.Nonce {
        var bytes = Data(count: 4)
        withUnsafeBytes(of: counter.bigEndian) { bytes.append(contentsOf: $0) }
        return try ChaChaPoly.Nonce(data: bytes)
    }

    // MARK: - Bridge signatures

    static func isValidBridgeSignature(_ signature: Data, for message: Data, bridgePublicKey: Data) -> Bool {
        guard let key = try? Curve25519.Signing.PublicKey(rawRepresentation: bridgePublicKey) else { return false }
        return key.isValidSignature(signature, for: message)
    }
}

/// `Authorization: HP2 id=…, ts=…, nonce=…, epk=…, sig=…`
public struct HP2Authorization: Equatable, Sendable {
    public var deviceId: String
    public var timestamp: Int
    public var nonce: String
    public var ephemeralPublicKey: String
    public var signature: String

    public init(deviceId: String, timestamp: Int, nonce: String, ephemeralPublicKey: String, signature: String) {
        self.deviceId = deviceId
        self.timestamp = timestamp
        self.nonce = nonce
        self.ephemeralPublicKey = ephemeralPublicKey
        self.signature = signature
    }

    public var headerValue: String {
        "\(HP2.authorizationScheme) id=\(deviceId), ts=\(timestamp), nonce=\(nonce), epk=\(ephemeralPublicKey), sig=\(signature)"
    }

    public init?(headerValue: String) {
        let prefix = HP2.authorizationScheme + " "
        guard headerValue.hasPrefix(prefix),
              let fields = HP2HeaderFields(String(headerValue.dropFirst(prefix.count))),
              let id = fields["id"], let ts = fields["ts"].flatMap(Int.init),
              let nonce = fields["nonce"], let epk = fields["epk"], let sig = fields["sig"]
        else { return nil }
        self.init(deviceId: id, timestamp: ts, nonce: nonce, ephemeralPublicKey: epk, signature: sig)
    }
}

/// `HP2-Bridge: epk=…, sig=…`
public struct HP2BridgeHeader: Equatable, Sendable {
    public var ephemeralPublicKey: String
    public var signature: String

    public init(ephemeralPublicKey: String, signature: String) {
        self.ephemeralPublicKey = ephemeralPublicKey
        self.signature = signature
    }

    public var headerValue: String { "epk=\(ephemeralPublicKey), sig=\(signature)" }

    public init?(headerValue: String) {
        guard let fields = HP2HeaderFields(headerValue),
              let epk = fields["epk"], let sig = fields["sig"]
        else { return nil }
        self.init(ephemeralPublicKey: epk, signature: sig)
    }
}

/// `key=value, key=value` with each key at most once.
private struct HP2HeaderFields {
    private var values: [String: String] = [:]

    init?(_ text: String) {
        for part in text.split(separator: ",") {
            let pair = part.trimmingCharacters(in: .whitespaces)
            guard let equals = pair.firstIndex(of: "=") else { return nil }
            let key = String(pair[..<equals])
            let value = String(pair[pair.index(after: equals)...])
            guard !key.isEmpty, !value.isEmpty, values[key] == nil else { return nil }
            values[key] = value
        }
    }

    subscript(key: String) -> String? { values[key] }
}

/// Seals and opens the frames of one WebSocket connection, each direction
/// with its own key and counter starting at 0 (device side).
public struct HP2FrameCipher: Sendable {
    private let sendKey: SymmetricKey
    private let receiveKey: SymmetricKey
    private var sendCounter: UInt64 = 0
    private var receiveCounter: UInt64 = 0

    public init(keys: HP2SessionKeys) {
        sendKey = keys.deviceToBridge
        receiveKey = keys.bridgeToDevice
    }

    public mutating func seal(_ plaintext: Data) throws -> Data {
        let frame = try HP2.seal(plaintext, key: sendKey, counter: sendCounter)
        sendCounter += 1
        return frame
    }

    /// Opens the next frame. Any tampering, reordering or replay fails,
    /// because the counter is part of the nonce.
    public mutating func open(_ frame: Data) throws(HP2Error) -> Data {
        let plaintext = try HP2.open(frame, key: receiveKey, counter: receiveCounter)
        receiveCounter += 1
        return plaintext
    }

    /// Plaintext of a signaling message frame.
    public static func jsonPlaintext(_ text: String) -> Data {
        var data = Data([HP2FrameType.json.rawValue])
        data.append(contentsOf: text.utf8)
        return data
    }
}
