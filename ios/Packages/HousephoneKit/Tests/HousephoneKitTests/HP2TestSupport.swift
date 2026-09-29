import CryptoKit
import Foundation
@testable import HousephoneKit

/// The bridge side of HP2 for tests: checks the device's request
/// signature, signs responses with its Ed25519 key, derives the session
/// keys and seals. Independent of the client code under test except for
/// the shared canonical messages, which the vector tests pin separately.
struct TestBridge: Sendable {
    let bridgeId: String
    let signingKey: Curve25519.Signing.PrivateKey

    init(bridgeId: String = "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e", signingKey: Curve25519.Signing.PrivateKey = .init()) {
        self.bridgeId = bridgeId
        self.signingKey = signingKey
    }

    var publicKey: Data { signingKey.publicKey.rawRepresentation }
    var fingerprint: String { HP2.fingerprint(of: publicKey) }

    /// Whether `authorization` is a valid device signature over this request.
    func verifiesDevice(
        authorization: String,
        method: String,
        pathAndQuery: String,
        body: Data,
        devicePublicKey: Data,
        now: Date = Date()
    ) -> Bool {
        guard let auth = HP2Authorization(headerValue: authorization),
              abs(Double(auth.timestamp) - now.timeIntervalSince1970) <= 60,
              let signature = HP2.data(base64URL: auth.signature).flatMap({ try? P256.Signing.ECDSASignature(rawRepresentation: $0) }),
              let key = try? P256.Signing.PublicKey(x963Representation: devicePublicKey)
        else { return false }
        let message = HP2.authMessage(
            method: method, pathAndQuery: pathAndQuery, bridgeId: bridgeId, deviceId: auth.deviceId,
            timestamp: auth.timestamp, nonce: auth.nonce, ephemeralPublicKey: auth.ephemeralPublicKey, body: body
        )
        return key.isValidSignature(signature, for: message)
    }

    /// Answers one signed request: a fresh ephemeral key, the session, the
    /// sealed (or plain) body and the `HP2-Bridge` header over it.
    func answer(authorization: String, status: Int, plaintext: Data?, seal: Bool = true, signWith key: Curve25519.Signing.PrivateKey? = nil) -> (header: String, body: Data, session: BridgeSession)? {
        guard let auth = HP2Authorization(headerValue: authorization),
              let nonce = HP2.data(base64URL: auth.nonce),
              let epkData = HP2.data(base64URL: auth.ephemeralPublicKey),
              let deviceEphemeral = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: epkData)
        else { return nil }
        let ephemeral = Curve25519.KeyAgreement.PrivateKey()
        let bridgeEpk = HP2.base64URL(ephemeral.publicKey.rawRepresentation)
        guard let shared = try? ephemeral.sharedSecretFromKeyAgreement(with: deviceEphemeral) else { return nil }
        let info = HP2.lines("HP2-KEYS", bridgeId, auth.deviceId, auth.ephemeralPublicKey, bridgeEpk)
        let okm = shared.hkdfDerivedSymmetricKey(using: SHA256.self, salt: nonce, sharedInfo: info, outputByteCount: 64)
            .withUnsafeBytes { Data($0) }
        let session = BridgeSession(keys: HP2SessionKeys(deviceToBridge: SymmetricKey(data: okm.prefix(32)), bridgeToDevice: SymmetricKey(data: okm.suffix(32))))

        var body = plaintext ?? Data()
        if seal, !body.isEmpty {
            body = session.sealBody(body)
        }
        let message = HP2.bridgeMessage(
            bridgeId: bridgeId, deviceId: auth.deviceId, nonce: auth.nonce,
            deviceEphemeralPublicKey: auth.ephemeralPublicKey, bridgeEphemeralPublicKey: bridgeEpk,
            status: status, body: body
        )
        let signature = try! (key ?? signingKey).signature(for: message)
        let header = HP2BridgeHeader(ephemeralPublicKey: bridgeEpk, signature: HP2.base64URL(signature)).headerValue
        return (header, body, session)
    }

    /// A `StubURLProtocol` response to a signed request. 2xx bodies are
    /// sealed; errors stay plain JSON, signed like everything else.
    func stubResponse(for request: URLRequest, status: Int, json: Data? = nil, extraHeaders: [String: String] = [:]) -> StubURLProtocol.Response {
        guard let authorization = request.value(forHTTPHeaderField: "Authorization"),
              let answer = answer(authorization: authorization, status: status, plaintext: json, seal: (200..<300).contains(status))
        else { return .init(status: 400, body: Data()) }
        var headers = extraHeaders
        headers[HP2.bridgeHeaderName] = answer.header
        if (200..<300).contains(status), !answer.body.isEmpty {
            headers["Content-Type"] = HP2.sealedContentType
        }
        return .init(status: status, body: answer.body, headers: headers)
    }
}

/// The bridge's end of one session: seals towards the device, opens what
/// the device sent, each with its own counter.
final class BridgeSession: @unchecked Sendable {
    let keys: HP2SessionKeys
    private let lock = NSLock()
    private var sendCounter: UInt64 = 0
    private var receiveCounter: UInt64 = 0

    init(keys: HP2SessionKeys) {
        self.keys = keys
    }

    func sealBody(_ plaintext: Data) -> Data {
        try! HP2.seal(plaintext, key: keys.bridgeToDevice, counter: 0)
    }

    func seal(_ plaintext: Data) -> Data {
        lock.withLock {
            defer { sendCounter += 1 }
            return try! HP2.seal(plaintext, key: keys.bridgeToDevice, counter: sendCounter)
        }
    }

    func open(_ frame: Data) throws -> Data {
        try lock.withLock {
            let plaintext = try HP2.open(frame, key: keys.deviceToBridge, counter: receiveCounter)
            receiveCounter += 1
            return plaintext
        }
    }
}

/// A device paired with `bridge`: credentials and its key in memory.
struct TestDevice: Sendable {
    let keyStore = InMemoryDeviceKeyStore()
    let key = SoftwareDeviceKey()
    let credentials: BridgeCredentials

    init(bridge: TestBridge, url: String = "wss://phone.example.com/v1/ws") {
        credentials = BridgeCredentials(
            bridgeURL: URL(string: url)!,
            deviceId: DeviceID(UUID(uuidString: "9B1D4C2A-5E6F-4A7B-8C9D-0E1F2A3B4C5D")!),
            bridgeId: bridge.bridgeId,
            bridgeName: "Zuhause",
            bridgePublicKey: bridge.publicKey,
            keyTag: "device"
        )
        keyStore.set(key, tag: "device")
    }
}

/// A valid v2 link to `bridge`.
func pairingLink(for bridge: TestBridge, code: String = "K7P2XH9QRMW4DZT8") -> PairingLink {
    PairingLink(bridgeURL: URL(string: "wss://phone.example.com/v1/ws")!, code: code, bridgeName: "Zuhause", fingerprint: bridge.fingerprint)
}
