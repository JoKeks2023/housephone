import Foundation

/// Administration from the app (ADR-0009). An admin request is an ordinary
/// HP2 request signed with the device key, plus a second signature in the
/// `HP2-Admin` header made with the admin key: a separate Secure Enclave
/// key that signs only after Face ID. Both cover the same fields under
/// different labels.
extension HP2 {
    public static let adminHeaderName = "HP2-Admin"

    /// What the admin key signs for a request: the fields of `authMessage`
    /// under the label `HP2-ADMIN`.
    public static func adminMessage(
        method: String,
        pathAndQuery: String,
        bridgeId: String,
        deviceId: String,
        timestamp: Int,
        nonce: String,
        ephemeralPublicKey: String,
        body: Data
    ) -> Data {
        lines("HP2-ADMIN", method.uppercased(), pathAndQuery, bridgeId, deviceId, String(timestamp), nonce, ephemeralPublicKey, sha256Hex(body))
    }

    /// What the admin key signs when it is enrolled, to prove the device
    /// holds it.
    public static func adminEnrollMessage(bridgeId: String, deviceId: String, adminKey: String) -> Data {
        lines("HP2-ADMIN-ENROLL", bridgeId, deviceId, adminKey)
    }
}

extension HP2Exchange {
    /// The `HP2-Admin` header value for this request, signed with the admin
    /// key (which asks for Face ID).
    public func adminSignature(key: any DeviceSigningKey, method: String, pathAndQuery: String, body: Data) throws -> String {
        let message = HP2.adminMessage(
            method: method,
            pathAndQuery: pathAndQuery,
            bridgeId: credentials.bridgeId,
            deviceId: authorization.deviceId,
            timestamp: authorization.timestamp,
            nonce: authorization.nonce,
            ephemeralPublicKey: authorization.ephemeralPublicKey,
            body: body
        )
        return HP2.base64URL(try key.signature(for: message))
    }
}

/// The body of `POST /v1/admin/enroll`.
public struct AdminEnrollment: Codable, Sendable, Equatable {
    /// The admin key (base64url X9.63 P-256).
    public var adminKey: String
    /// The admin key's signature over `HP2.adminEnrollMessage`.
    public var proof: String

    public init(adminKey: String, proof: String) {
        self.adminKey = adminKey
        self.proof = proof
    }

    /// Signs the enrollment of `key` (asks for Face ID once).
    public static func make(key: any DeviceSigningKey, credentials: BridgeCredentials) throws -> AdminEnrollment {
        let publicKey = HP2.base64URL(key.publicKeyX963)
        let proof = try key.signature(for: HP2.adminEnrollMessage(
            bridgeId: credentials.bridgeId,
            deviceId: credentials.deviceId.description,
            adminKey: publicKey
        ))
        return AdminEnrollment(adminKey: publicKey, proof: HP2.base64URL(proof))
    }
}
