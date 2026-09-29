import CryptoKit
import Foundation
import Security

/// Signs requests for one paired bridge (ADR-0004).
public struct HP2Signer: Sendable {
    public let credentials: BridgeCredentials
    public let key: any DeviceSigningKey
    private let now: @Sendable () -> Date

    public init(credentials: BridgeCredentials, key: any DeviceSigningKey, now: @escaping @Sendable () -> Date = { Date() }) {
        self.credentials = credentials
        self.key = key
        self.now = now
    }

    /// The path and query exactly as sent, which is what the bridge sees as
    /// the request URI (e.g. `/v1/history?limit=100`).
    public static func pathAndQuery(of url: URL) -> String {
        var path = url.path(percentEncoded: true)
        if path.isEmpty { path = "/" }
        if let query = url.query(percentEncoded: true) {
            path += "?" + query
        }
        return path
    }

    /// Signs a request with a fresh nonce and ephemeral key.
    public func exchange(method: String, url: URL, body: Data?) throws -> HP2Exchange {
        var nonce = Data(count: HP2.nonceLength)
        let status = nonce.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, HP2.nonceLength, $0.baseAddress!) }
        guard status == errSecSuccess else { throw KeychainError(status: status) }
        return try exchange(
            method: method,
            pathAndQuery: Self.pathAndQuery(of: url),
            body: body ?? Data(),
            timestamp: Int(now().timeIntervalSince1970),
            nonce: nonce,
            ephemeral: Curve25519.KeyAgreement.PrivateKey()
        )
    }

    /// Deterministic variant, e.g. for the test vectors.
    public func exchange(
        method: String,
        pathAndQuery: String,
        body: Data,
        timestamp: Int,
        nonce: Data,
        ephemeral: Curve25519.KeyAgreement.PrivateKey
    ) throws -> HP2Exchange {
        let deviceId = credentials.deviceId.description
        let nonceText = HP2.base64URL(nonce)
        let epk = HP2.base64URL(ephemeral.publicKey.rawRepresentation)
        let message = HP2.authMessage(
            method: method,
            pathAndQuery: pathAndQuery,
            bridgeId: credentials.bridgeId,
            deviceId: deviceId,
            timestamp: timestamp,
            nonce: nonceText,
            ephemeralPublicKey: epk,
            body: body
        )
        let signature = try key.signature(for: message)
        let authorization = HP2Authorization(
            deviceId: deviceId,
            timestamp: timestamp,
            nonce: nonceText,
            ephemeralPublicKey: epk,
            signature: HP2.base64URL(signature)
        )
        return HP2Exchange(authorization: authorization, nonce: nonce, ephemeral: ephemeral, credentials: credentials)
    }
}

/// One signed request and what's needed to check its response.
public struct HP2Exchange: Sendable {
    public let authorization: HP2Authorization
    let nonce: Data
    let ephemeral: Curve25519.KeyAgreement.PrivateKey
    let credentials: BridgeCredentials

    /// Checks the bridge's signature over the response against the pinned
    /// key and derives the session keys. Every response must pass this,
    /// errors and `101` included; there is no fallback.
    public func verifyResponse(status: Int, bridgeHeader: String?, body: Data) throws(HP2Error) -> HP2SessionKeys {
        guard let bridgeHeader else { throw .missingBridgeSignature }
        guard let header = HP2BridgeHeader(headerValue: bridgeHeader),
              let signature = HP2.data(base64URL: header.signature),
              let epkData = HP2.data(base64URL: header.ephemeralPublicKey),
              let bridgeEphemeral = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: epkData)
        else { throw .invalidBridgeSignature }

        let message = HP2.bridgeMessage(
            bridgeId: credentials.bridgeId,
            deviceId: authorization.deviceId,
            nonce: authorization.nonce,
            deviceEphemeralPublicKey: authorization.ephemeralPublicKey,
            bridgeEphemeralPublicKey: header.ephemeralPublicKey,
            status: status,
            body: body
        )
        guard HP2.isValidBridgeSignature(signature, for: message, bridgePublicKey: credentials.bridgePublicKey) else {
            throw .invalidBridgeSignature
        }
        do {
            return try HP2.deriveKeys(
                devicePrivateKey: ephemeral,
                bridgePublicKey: bridgeEphemeral,
                nonce: nonce,
                bridgeId: credentials.bridgeId,
                deviceId: authorization.deviceId
            )
        } catch {
            throw .invalidBridgeSignature
        }
    }

    /// Opens a sealed HTTPS response body (bridge → device, counter 0).
    public func openBody(_ body: Data, keys: HP2SessionKeys) throws(HP2Error) -> Data {
        try HP2.open(body, key: keys.bridgeToDevice, counter: 0)
    }
}

/// Pairing v2 (`POST /v1/pair`): the proof the device sends and the check
/// of the bridge's answer against the QR code's fingerprint.
public enum HP2Pairing {
    /// The request body, signed with the new device key.
    public static func request(
        link: PairingLink,
        deviceName: String,
        platform: DevicePlatform,
        model: String?,
        key: any DeviceSigningKey,
        nonce: Data
    ) throws -> PairRequest {
        let publicKey = HP2.base64URL(key.publicKeyX963)
        let nonceText = HP2.base64URL(nonce)
        let proof = try key.signature(for: HP2.pairProofMessage(code: link.code, nonce: nonceText, publicKey: publicKey))
        return PairRequest(
            code: link.code,
            deviceName: deviceName,
            platform: platform,
            model: model,
            publicKey: publicKey,
            nonce: nonceText,
            proof: HP2.base64URL(proof)
        )
    }

    /// Accepts the bridge's answer only if its key matches the fingerprint
    /// from the QR code and it signed this exact pairing.
    public static func credentials(
        from result: PairingResult,
        request: PairRequest,
        link: PairingLink,
        keyTag: String
    ) throws(HP2Error) -> BridgeCredentials {
        guard let bridgePublicKey = HP2.data(base64URL: result.bridgePublicKey), bridgePublicKey.count == 32 else {
            throw .bridgeIdentityMismatch
        }
        guard HP2.fingerprint(of: bridgePublicKey) == link.fingerprint else { throw .bridgeIdentityMismatch }
        let message = HP2.pairResponseMessage(
            bridgeId: result.bridgeId,
            deviceId: result.deviceId.description,
            publicKey: request.publicKey,
            nonce: request.nonce,
            code: request.code
        )
        guard let signature = HP2.data(base64URL: result.signature),
              HP2.isValidBridgeSignature(signature, for: message, bridgePublicKey: bridgePublicKey)
        else { throw .bridgeIdentityMismatch }
        return BridgeCredentials(
            bridgeURL: link.bridgeURL,
            lanURL: link.lanURL,
            deviceId: result.deviceId,
            bridgeId: result.bridgeId,
            bridgeName: result.bridgeName,
            bridgePublicKey: bridgePublicKey,
            keyTag: keyTag
        )
    }
}
