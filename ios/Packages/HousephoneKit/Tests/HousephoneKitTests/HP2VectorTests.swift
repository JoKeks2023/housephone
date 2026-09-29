import CryptoKit
import Foundation
import Testing
@testable import HousephoneKit

/// HP2 against `docs/protocol/fixtures/crypto/hp2-vectors.json`, which the
/// Go bridge tests against as well. Everything except the ECDSA signatures
/// (random) must reproduce byte for byte.
struct HP2VectorTests {
    struct Vectors: Decodable {
        struct Frame: Decodable {
            var direction: String
            var counter: String
            var plaintextHex: String
            var sealedHex: String
        }

        var ids: [String: String]
        var keys: [String: String]
        var pairing: [String: String]
        var request: [String: String]
        var keyDerivation: [String: String]
        var response: [String: String]
        var frames: [Frame]
    }

    static let vectors: Vectors = {
        var url = URL(filePath: #filePath)
        for _ in 0..<6 { url.deleteLastPathComponent() }
        let file = url.appending(path: "docs/protocol/fixtures/crypto/hp2-vectors.json")
        return try! JSONDecoder().decode(Vectors.self, from: Data(contentsOf: file))
    }()

    var v: Vectors { Self.vectors }
    var bridgeId: String { v.ids["bridgeId"]! }
    var deviceId: String { v.ids["deviceId"]! }

    static func hex(_ string: String) -> Data {
        var data = Data()
        var index = string.startIndex
        while index < string.endIndex {
            let next = string.index(index, offsetBy: 2)
            data.append(UInt8(string[index..<next], radix: 16)!)
            index = next
        }
        return data
    }

    static func hex(_ data: Data) -> String { data.map { String(format: "%02x", $0) }.joined() }
    static func hex(_ key: SymmetricKey) -> String { key.withUnsafeBytes { hex(Data($0)) } }

    var deviceKey: SoftwareDeviceKey { try! SoftwareDeviceKey(rawRepresentation: Self.hex(v.keys["devicePrivateKeyScalarHex"]!)) }
    var deviceEphemeral: Curve25519.KeyAgreement.PrivateKey {
        try! Curve25519.KeyAgreement.PrivateKey(rawRepresentation: Self.hex(v.keys["deviceEphemeralPrivateX25519Hex"]!))
    }
    var bridgePublicKey: Data { HP2.data(base64URL: v.keys["bridgePublicKey"]!)! }

    var credentials: BridgeCredentials {
        BridgeCredentials(
            bridgeURL: URL(string: "wss://phone.example.com/v1/ws")!,
            deviceId: DeviceID(string: deviceId)!,
            bridgeId: bridgeId,
            bridgeName: "Zuhause",
            bridgePublicKey: bridgePublicKey,
            keyTag: "vector"
        )
    }

    /// The vector request, rebuilt with the vector's nonce and ephemeral key.
    func vectorExchange() throws -> HP2Exchange {
        try HP2Signer(credentials: credentials, key: deviceKey).exchange(
            method: v.request["method"]!,
            pathAndQuery: v.request["path"]!,
            body: Data(v.request["body"]!.utf8),
            timestamp: Int(v.request["ts"]!)!,
            nonce: HP2.data(base64URL: v.request["nonce"]!)!,
            ephemeral: deviceEphemeral
        )
    }

    // MARK: Keys and encodings

    @Test func deviceAndBridgeKeys() {
        #expect(HP2.base64URL(deviceKey.publicKeyX963) == v.keys["devicePublicKeyX963"])
        #expect(HP2.fingerprint(of: bridgePublicKey) == v.keys["bridgeFingerprint"])
        #expect(HP2.base64URL(deviceEphemeral.publicKey.rawRepresentation) == v.keys["deviceEphemeralPublic"])
        let bridgeEphemeral = try! Curve25519.KeyAgreement.PrivateKey(rawRepresentation: Self.hex(v.keys["bridgeEphemeralPrivateX25519Hex"]!))
        #expect(HP2.base64URL(bridgeEphemeral.publicKey.rawRepresentation) == v.keys["bridgeEphemeralPublic"])
    }

    @Test func base64URLIsStrict() {
        #expect(HP2.base64URL(Data([0xFB, 0xFF])) == "-_8")
        #expect(HP2.data(base64URL: "-_8") == Data([0xFB, 0xFF]))
        #expect(HP2.data(base64URL: "+/8") == nil)
        #expect(HP2.data(base64URL: "-_8=") == nil)
        #expect(HP2.data(base64URL: "") == nil)
    }

    // MARK: Pairing

    @Test func pairingMessagesAndSignatures() throws {
        let p = v.pairing
        let publicKey = v.keys["devicePublicKeyX963"]!
        let proof = HP2.pairProofMessage(code: p["code"]!, nonce: p["nonce"]!, publicKey: publicKey)
        #expect(String(decoding: proof, as: UTF8.self) == p["proofMessage"])
        let proofSignature = try P256.Signing.ECDSASignature(rawRepresentation: HP2.data(base64URL: p["proofSignature"]!)!)
        #expect(try P256.Signing.PublicKey(x963Representation: deviceKey.publicKeyX963).isValidSignature(proofSignature, for: proof))

        let response = HP2.pairResponseMessage(bridgeId: bridgeId, deviceId: deviceId, publicKey: publicKey, nonce: p["nonce"]!, code: p["code"]!)
        #expect(String(decoding: response, as: UTF8.self) == p["responseMessage"])
        #expect(HP2.isValidBridgeSignature(HP2.data(base64URL: p["responseSignature"]!)!, for: response, bridgePublicKey: bridgePublicKey))
    }

    @Test func pairingLinkFromVector() throws {
        let link = try PairingLink(string: v.pairing["link"]!)
        #expect(link.code == v.pairing["code"])
        #expect(link.fingerprint == v.keys["bridgeFingerprint"])
        #expect(link.bridgeName == "Zuhause")
        #expect(link.groupedCode == "K7P2-XH9Q-RMW4-DZT8")
    }

    @Test func pairingResponseIsAcceptedOnlyForThePinnedBridge() throws {
        let p = v.pairing
        let link = try PairingLink(string: p["link"]!)
        let request = PairRequest(
            code: p["code"]!, deviceName: "iPhone", platform: .ios, model: nil,
            publicKey: v.keys["devicePublicKeyX963"]!, nonce: p["nonce"]!, proof: p["proofSignature"]!
        )
        let result = PairingResult(
            deviceId: DeviceID(string: deviceId)!, bridgeId: bridgeId, bridgeName: "Zuhause",
            bridgePublicKey: v.keys["bridgePublicKey"]!, signature: p["responseSignature"]!
        )
        let credentials = try HP2Pairing.credentials(from: result, request: request, link: link, keyTag: "t")
        #expect(credentials.bridgePublicKey == bridgePublicKey)
        #expect(credentials.bridgeFingerprint == link.fingerprint)

        // Another bridge answering for the same link.
        let impostor = Curve25519.Signing.PrivateKey()
        var forged = result
        forged.bridgePublicKey = HP2.base64URL(impostor.publicKey.rawRepresentation)
        forged.signature = HP2.base64URL(try impostor.signature(for: HP2.pairResponseMessage(
            bridgeId: bridgeId, deviceId: deviceId, publicKey: request.publicKey, nonce: request.nonce, code: request.code
        )))
        #expect(throws: HP2Error.bridgeIdentityMismatch) { try HP2Pairing.credentials(from: forged, request: request, link: link, keyTag: "t") }

        // The right key, but a signature over another pairing (other nonce).
        var otherRequest = request
        otherRequest.nonce = HP2.base64URL(Data(repeating: 7, count: 16))
        #expect(throws: HP2Error.bridgeIdentityMismatch) { try HP2Pairing.credentials(from: result, request: otherRequest, link: link, keyTag: "t") }
    }

    // MARK: Request

    @Test func requestSignatureInputAndHeader() throws {
        let r = v.request
        #expect(HP2.sha256Hex(Data(r["body"]!.utf8)) == r["bodySha256Hex"])
        let message = HP2.authMessage(
            method: "put", pathAndQuery: r["path"]!, bridgeId: bridgeId, deviceId: deviceId, timestamp: Int(r["ts"]!)!,
            nonce: r["nonce"]!, ephemeralPublicKey: v.keys["deviceEphemeralPublic"]!, body: Data(r["body"]!.utf8)
        )
        #expect(String(decoding: message, as: UTF8.self) == r["signedMessage"])

        // The vector's (random) signature verifies over that input …
        let vectorSignature = try P256.Signing.ECDSASignature(rawRepresentation: HP2.data(base64URL: r["signature"]!)!)
        let devicePublic = try P256.Signing.PublicKey(x963Representation: deviceKey.publicKeyX963)
        #expect(devicePublic.isValidSignature(vectorSignature, for: message))
        // … and the header parses to the same fields.
        let parsed = try #require(HP2Authorization(headerValue: r["authorizationHeader"]!))
        #expect(parsed.deviceId == deviceId)
        #expect(parsed.timestamp == Int(r["ts"]!))
        #expect(parsed.nonce == r["nonce"])
        #expect(parsed.ephemeralPublicKey == v.keys["deviceEphemeralPublic"])
        #expect(parsed.headerValue == r["authorizationHeader"])

        // Our own signer produces the same header apart from the signature.
        let exchange = try vectorExchange()
        var expected = parsed
        expected.signature = exchange.authorization.signature
        #expect(exchange.authorization == expected)
        let ours = try P256.Signing.ECDSASignature(rawRepresentation: HP2.data(base64URL: exchange.authorization.signature)!)
        #expect(devicePublic.isValidSignature(ours, for: message))
    }

    // MARK: Keys, response, frames

    @Test func keyDerivation() throws {
        let k = v.keyDerivation
        let bridgeEphemeral = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: HP2.data(base64URL: v.keys["bridgeEphemeralPublic"]!)!)
        let shared = try deviceEphemeral.sharedSecretFromKeyAgreement(with: bridgeEphemeral)
        #expect(shared.withUnsafeBytes { Self.hex(Data($0)) } == k["sharedSecretHex"])
        #expect(String(decoding: HP2.lines("HP2-KEYS", bridgeId, deviceId, v.keys["deviceEphemeralPublic"]!, v.keys["bridgeEphemeralPublic"]!), as: UTF8.self) == k["hkdfInfo"])

        let keys = try HP2.deriveKeys(
            devicePrivateKey: deviceEphemeral, bridgePublicKey: bridgeEphemeral,
            nonce: Self.hex(k["hkdfSaltHex"]!), bridgeId: bridgeId, deviceId: deviceId
        )
        #expect(Self.hex(keys.deviceToBridge) + Self.hex(keys.bridgeToDevice) == k["okmHex"])
        #expect(Self.hex(keys.deviceToBridge) == k["deviceToBridgeKeyHex"])
        #expect(Self.hex(keys.bridgeToDevice) == k["bridgeToDeviceKeyHex"])
    }

    @Test func sealedResponseVerifiesAndOpens() throws {
        let r = v.response
        let sealed = Self.hex(r["sealedBodyHex"]!)
        let keys = HP2SessionKeys(
            deviceToBridge: SymmetricKey(data: Self.hex(v.keyDerivation["deviceToBridgeKeyHex"]!)),
            bridgeToDevice: SymmetricKey(data: Self.hex(v.keyDerivation["bridgeToDeviceKeyHex"]!))
        )
        #expect(Self.hex(try HP2.seal(Data(r["plaintextBody"]!.utf8), key: keys.bridgeToDevice, counter: 0)) == r["sealedBodyHex"])

        let message = HP2.bridgeMessage(
            bridgeId: bridgeId, deviceId: deviceId, nonce: v.request["nonce"]!,
            deviceEphemeralPublicKey: v.keys["deviceEphemeralPublic"]!, bridgeEphemeralPublicKey: v.keys["bridgeEphemeralPublic"]!,
            status: 200, body: sealed
        )
        #expect(String(decoding: message, as: UTF8.self) == r["signedMessage"])

        // The full client path: header check against the pinned key, keys, body.
        let exchange = try vectorExchange()
        let derived = try exchange.verifyResponse(status: 200, bridgeHeader: r["bridgeHeader"], body: sealed)
        #expect(Self.hex(derived.bridgeToDevice) == v.keyDerivation["bridgeToDeviceKeyHex"])
        #expect(String(decoding: try exchange.openBody(sealed, keys: derived), as: UTF8.self) == r["plaintextBody"])

        // Upgrade (101, empty body).
        let upgradeHeader = HP2BridgeHeader(ephemeralPublicKey: v.keys["bridgeEphemeralPublic"]!, signature: r["upgrade101Signature"]!).headerValue
        #expect(String(decoding: HP2.bridgeMessage(
            bridgeId: bridgeId, deviceId: deviceId, nonce: v.request["nonce"]!,
            deviceEphemeralPublicKey: v.keys["deviceEphemeralPublic"]!, bridgeEphemeralPublicKey: v.keys["bridgeEphemeralPublic"]!,
            status: 101, body: Data()
        ), as: UTF8.self) == r["upgrade101SignedMessage"])
        _ = try exchange.verifyResponse(status: 101, bridgeHeader: upgradeHeader, body: Data())
    }

    @Test func framesInOrder() throws {
        let keys = HP2SessionKeys(
            deviceToBridge: SymmetricKey(data: Self.hex(v.keyDerivation["deviceToBridgeKeyHex"]!)),
            bridgeToDevice: SymmetricKey(data: Self.hex(v.keyDerivation["bridgeToDeviceKeyHex"]!))
        )
        var cipher = HP2FrameCipher(keys: keys)
        for frame in v.frames {
            let plaintext = Self.hex(frame.plaintextHex)
            switch frame.direction {
            case "deviceToBridge":
                #expect(Self.hex(try cipher.seal(plaintext)) == frame.sealedHex, "frame \(frame.direction) #\(frame.counter)")
            default:
                #expect(try cipher.open(Self.hex(frame.sealedHex)) == plaintext)
            }
        }
        #expect(HP2FrameCipher.jsonPlaintext(#"{"a":1}"#).first == HP2FrameType.json.rawValue)
    }

    // MARK: Attacks

    @Test func responseSignatureAttacksFail() throws {
        let r = v.response
        let sealed = Self.hex(r["sealedBodyHex"]!)
        let exchange = try vectorExchange()

        // No header, garbage header.
        #expect(throws: HP2Error.missingBridgeSignature) { try exchange.verifyResponse(status: 200, bridgeHeader: nil, body: sealed) }
        #expect(throws: HP2Error.invalidBridgeSignature) { try exchange.verifyResponse(status: 200, bridgeHeader: "sig=x", body: sealed) }
        // Status or body changed after signing.
        #expect(throws: HP2Error.invalidBridgeSignature) { try exchange.verifyResponse(status: 201, bridgeHeader: r["bridgeHeader"], body: sealed) }
        var tampered = sealed
        tampered[tampered.startIndex] ^= 0x01
        #expect(throws: HP2Error.invalidBridgeSignature) { try exchange.verifyResponse(status: 200, bridgeHeader: r["bridgeHeader"], body: tampered) }

        // A valid signature, but by another key than the pinned one.
        let impostor = Curve25519.Signing.PrivateKey()
        let message = HP2.bridgeMessage(
            bridgeId: bridgeId, deviceId: deviceId, nonce: v.request["nonce"]!,
            deviceEphemeralPublicKey: v.keys["deviceEphemeralPublic"]!, bridgeEphemeralPublicKey: v.keys["bridgeEphemeralPublic"]!,
            status: 200, body: sealed
        )
        let forged = HP2BridgeHeader(ephemeralPublicKey: v.keys["bridgeEphemeralPublic"]!, signature: HP2.base64URL(try impostor.signature(for: message)))
        #expect(throws: HP2Error.invalidBridgeSignature) { try exchange.verifyResponse(status: 200, bridgeHeader: forged.headerValue, body: sealed) }

        // The bridge's genuine answer to another request (replayed response).
        let other = try HP2Signer(credentials: credentials, key: deviceKey).exchange(method: "PUT", url: URL(string: "https://phone.example.com/v1/device")!, body: Data())
        #expect(throws: HP2Error.invalidBridgeSignature) { try other.verifyResponse(status: 200, bridgeHeader: r["bridgeHeader"], body: sealed) }
    }

    @Test func frameAttacksFail() throws {
        let keys = HP2SessionKeys(
            deviceToBridge: SymmetricKey(data: Self.hex(v.keyDerivation["deviceToBridgeKeyHex"]!)),
            bridgeToDevice: SymmetricKey(data: Self.hex(v.keyDerivation["bridgeToDeviceKeyHex"]!))
        )
        let first = try HP2.seal(Data([0x00, 0x7B, 0x7D]), key: keys.bridgeToDevice, counter: 0)
        let second = try HP2.seal(Data([0x00, 0x5B, 0x5D]), key: keys.bridgeToDevice, counter: 1)

        // Tampered.
        var cipher = HP2FrameCipher(keys: keys)
        var tampered = first
        tampered[tampered.index(before: tampered.endIndex)] ^= 0x80
        #expect(throws: HP2Error.invalidFrame) { try cipher.open(tampered) }

        // Reordered: the second frame first.
        cipher = HP2FrameCipher(keys: keys)
        #expect(throws: HP2Error.invalidFrame) { try cipher.open(second) }

        // Replayed: the first frame twice.
        cipher = HP2FrameCipher(keys: keys)
        _ = try cipher.open(first)
        #expect(throws: HP2Error.invalidFrame) { try cipher.open(first) }

        // Reflected: a frame the device itself sealed (other key).
        cipher = HP2FrameCipher(keys: keys)
        let reflected = try HP2.seal(Data([0x00]), key: keys.deviceToBridge, counter: 0)
        #expect(throws: HP2Error.invalidFrame) { try cipher.open(reflected) }

        // Too short to carry a tag.
        #expect(throws: HP2Error.invalidFrame) { try HP2.open(Data(repeating: 0, count: 8), key: keys.bridgeToDevice, counter: 0) }
    }
}
