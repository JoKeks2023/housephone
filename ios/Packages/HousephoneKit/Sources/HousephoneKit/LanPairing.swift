import CryptoKit
import Foundation

// Pairing in the home network without a QR code (signaling v2.1,
// ADR-0007). The device finds the bridge via Bonjour, both sides show a
// six-digit confirmation code (SAS), and an admin approves on the bridge.
//
// 1. device → bridge: device key + commitment to (ephemeral key, nonce)
// 2. bridge → device: bridge key, ephemeral key and nonce, signed
// 3. device → bridge: ephemeral key and nonce, proof over the transcript
//
// The commitment means neither side can pick its values after seeing the
// other's, so a man in the middle hits the right code with 10^-6.

/// Body of `POST /v1/pair/lan`.
public struct LanPairStart: Codable, Sendable, Equatable {
    public var deviceName: String
    public var platform: DevicePlatform
    public var model: String?
    public var publicKey: String
    public var commitment: String
}

/// Answer of `POST /v1/pair/lan`, signed by the bridge.
public struct LanPairOffer: Codable, Sendable, Equatable {
    public var pairingId: String
    public var bridgeId: String
    public var bridgeName: String
    public var bridgePublicKey: String
    public var bridgeEphemeral: String
    public var bridgeNonce: String
    public var expiresAt: Date
    public var signature: String

    public init(pairingId: String, bridgeId: String, bridgeName: String, bridgePublicKey: String, bridgeEphemeral: String, bridgeNonce: String, expiresAt: Date, signature: String) {
        self.pairingId = pairingId
        self.bridgeId = bridgeId
        self.bridgeName = bridgeName
        self.bridgePublicKey = bridgePublicKey
        self.bridgeEphemeral = bridgeEphemeral
        self.bridgeNonce = bridgeNonce
        self.expiresAt = expiresAt
        self.signature = signature
    }
}

/// Body of `POST /v1/pair/lan/{pairingId}/reveal`.
public struct LanPairReveal: Codable, Sendable, Equatable {
    public var deviceEphemeral: String
    public var deviceNonce: String
    public var proof: String
}

/// State of a request: answer of the reveal and of `GET /v1/pair/lan/{id}`.
public struct LanPairState: Codable, Sendable, Equatable {
    public enum Status: String, Codable, Sendable {
        case pending, approved, denied, expired
    }

    public var status: Status
    public var expiresAt: Date?
    /// The sealed `LanPairApproval` once approved (base64url).
    public var sealed: String?

    public init(status: Status, expiresAt: Date? = nil, sealed: String? = nil) {
        self.status = status
        self.expiresAt = expiresAt
        self.sealed = sealed
    }
}

/// The sealed, signed result of an approved request.
public struct LanPairApproval: Codable, Sendable, Equatable {
    public var deviceId: String
    public var bridgeId: String
    public var bridgeName: String
    public var publicUrl: String
    public var lanUrl: String
    public var signature: String
}

extension HP2 {
    /// Length of the confirmation code.
    public static let lanSASDigits = 6

    public static func lanCommitment(publicKey: String, deviceEphemeral: String, deviceNonce: String) -> String {
        base64URL(Data(SHA256.hash(data: lines("HP2-LAN-COMMIT", publicKey, deviceEphemeral, deviceNonce))))
    }

    public static func lanOfferMessage(pairingId: String, bridgeId: String, publicKey: String, commitment: String, bridgePublicKey: String, bridgeEphemeral: String, bridgeNonce: String) -> Data {
        lines("HP2-LAN-OFFER", pairingId, bridgeId, publicKey, commitment, bridgePublicKey, bridgeEphemeral, bridgeNonce)
    }

    // swiftlint:disable:next function_parameter_count
    public static func lanTranscriptHash(pairingId: String, bridgeId: String, publicKey: String, commitment: String, bridgePublicKey: String, bridgeEphemeral: String, bridgeNonce: String, deviceEphemeral: String, deviceNonce: String) -> Data {
        Data(SHA256.hash(data: lines(
            "HP2-LAN-TRANSCRIPT", pairingId, bridgeId, publicKey, commitment,
            bridgePublicKey, bridgeEphemeral, bridgeNonce, deviceEphemeral, deviceNonce
        )))
    }

    public static func lanProofMessage(transcriptHash: Data) -> Data {
        lines("HP2-LAN-PROOF", base64URL(transcriptHash))
    }

    public static func lanApprovedMessage(transcriptHash: Data, deviceId: String, bridgeId: String, publicUrl: String, lanUrl: String) -> Data {
        lines("HP2-LAN-APPROVED", base64URL(transcriptHash), deviceId, bridgeId, publicUrl, lanUrl)
    }

    /// The confirmation code and the key that seals the approval.
    public static func lanSecrets(sharedSecret: SharedSecret, transcriptHash: Data) -> (sas: String, sealKey: SymmetricKey) {
        let sas = sharedSecret.hkdfDerivedSymmetricKey(using: SHA256.self, salt: transcriptHash, sharedInfo: Data("HP2-LAN-SAS".utf8), outputByteCount: 4)
        let value = sas.withUnsafeBytes { bytes in bytes.reduce(UInt32(0)) { $0 << 8 | UInt32($1) } }
        let key = sharedSecret.hkdfDerivedSymmetricKey(using: SHA256.self, salt: transcriptHash, sharedInfo: Data("HP2-LAN-SEAL".utf8), outputByteCount: 32)
        return (formatSAS(value), key)
    }

    /// Six digits, zero-padded.
    public static func formatSAS(_ value: UInt32) -> String {
        let digits = String(value % 1_000_000)
        return String(repeating: "0", count: lanSASDigits - digits.count) + digits
    }

    /// "123456" → "123 456" for reading it out.
    public static func groupedSAS(_ sas: String) -> String {
        guard sas.count == lanSASDigits else { return sas }
        return String(sas.prefix(3)) + " " + String(sas.suffix(3))
    }
}

/// The device side of one LAN pairing: holds the ephemeral key and the
/// nonce until the bridge answered, then the SAS and the seal key.
public struct HP2LanPairing: Sendable {
    public let start: LanPairStart
    private let ephemeral: Curve25519.KeyAgreement.PrivateKey
    private let nonce: String

    public private(set) var offer: LanPairOffer?
    public private(set) var sas: String?
    private var transcriptHash: Data?
    private var sealKey: SymmetricKey?
    private var bridgePublicKey: Data?

    public init(
        key: any DeviceSigningKey,
        deviceName: String,
        model: String?,
        ephemeral: Curve25519.KeyAgreement.PrivateKey = .init(),
        nonce: Data
    ) {
        let publicKey = HP2.base64URL(key.publicKeyX963)
        let nonceText = HP2.base64URL(nonce)
        self.ephemeral = ephemeral
        self.nonce = nonceText
        start = LanPairStart(
            deviceName: deviceName,
            platform: .ios,
            model: model,
            publicKey: publicKey,
            commitment: HP2.lanCommitment(
                publicKey: publicKey,
                deviceEphemeral: HP2.base64URL(ephemeral.publicKey.rawRepresentation),
                deviceNonce: nonceText
            )
        )
    }

    /// Checks the bridge's signed offer, derives the SAS and returns the
    /// reveal, signed with the device key.
    public mutating func accept(_ offer: LanPairOffer, key: any DeviceSigningKey) throws -> LanPairReveal {
        guard let bridgeKey = HP2.data(base64URL: offer.bridgePublicKey), bridgeKey.count == 32,
              let signature = HP2.data(base64URL: offer.signature),
              HP2.isValidBridgeSignature(signature, for: HP2.lanOfferMessage(
                  pairingId: offer.pairingId, bridgeId: offer.bridgeId, publicKey: start.publicKey,
                  commitment: start.commitment, bridgePublicKey: offer.bridgePublicKey,
                  bridgeEphemeral: offer.bridgeEphemeral, bridgeNonce: offer.bridgeNonce
              ), bridgePublicKey: bridgeKey),
              let peerRaw = HP2.data(base64URL: offer.bridgeEphemeral),
              let peer = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: peerRaw),
              HP2.data(base64URL: offer.bridgeNonce)?.count == HP2.nonceLength,
              let shared = try? ephemeral.sharedSecretFromKeyAgreement(with: peer)
        else { throw HP2Error.bridgeIdentityMismatch }

        let deviceEphemeral = HP2.base64URL(ephemeral.publicKey.rawRepresentation)
        let hash = HP2.lanTranscriptHash(
            pairingId: offer.pairingId, bridgeId: offer.bridgeId, publicKey: start.publicKey,
            commitment: start.commitment, bridgePublicKey: offer.bridgePublicKey,
            bridgeEphemeral: offer.bridgeEphemeral, bridgeNonce: offer.bridgeNonce,
            deviceEphemeral: deviceEphemeral, deviceNonce: nonce
        )
        let secrets = HP2.lanSecrets(sharedSecret: shared, transcriptHash: hash)
        let proof = try key.signature(for: HP2.lanProofMessage(transcriptHash: hash))
        self.offer = offer
        sas = secrets.sas
        sealKey = secrets.sealKey
        transcriptHash = hash
        bridgePublicKey = bridgeKey
        return LanPairReveal(deviceEphemeral: deviceEphemeral, deviceNonce: nonce, proof: HP2.base64URL(proof))
    }

    /// Opens and checks the approval and builds the credentials to store.
    /// `discoveredURL` is the private listener the device talked to; it is
    /// used when the bridge has no public URL.
    public func credentials(from state: LanPairState, discoveredURL: URL, keyTag: String) throws -> BridgeCredentials {
        guard state.status == .approved, let offer, let sealKey, let transcriptHash, let bridgePublicKey,
              let sealedText = state.sealed, let sealed = HP2.data(base64URL: sealedText),
              let plain = try? HP2.open(sealed, key: sealKey, counter: 0),
              let approval = try? JSONDecoder().decode(LanPairApproval.self, from: plain),
              approval.bridgeId == offer.bridgeId,
              let signature = HP2.data(base64URL: approval.signature),
              HP2.isValidBridgeSignature(signature, for: HP2.lanApprovedMessage(
                  transcriptHash: transcriptHash, deviceId: approval.deviceId, bridgeId: approval.bridgeId,
                  publicUrl: approval.publicUrl, lanUrl: approval.lanUrl
              ), bridgePublicKey: bridgePublicKey),
              let deviceId = DeviceID(string: approval.deviceId)
        else { throw HP2Error.bridgeIdentityMismatch }

        let lanURL = URL(string: approval.lanUrl).flatMap { $0.host() == nil ? nil : $0 } ?? discoveredURL
        let publicURL = URL(string: approval.publicUrl).flatMap { $0.host() == nil ? nil : $0 } ?? lanURL
        return BridgeCredentials(
            bridgeURL: publicURL,
            lanURL: lanURL,
            deviceId: deviceId,
            bridgeId: approval.bridgeId,
            bridgeName: approval.bridgeName,
            bridgePublicKey: bridgePublicKey,
            keyTag: keyTag
        )
    }
}

/// A LAN pairing waiting for the admin; show `sas` until it ends.
public struct LanPairingSession: Sendable {
    /// The private listener (`ws://host:port/v1/ws`).
    public let lanURL: URL
    public let keyTag: String
    public let pairing: HP2LanPairing
    /// The six-digit confirmation code.
    public let sas: String
    public let bridgeName: String
    public let expiresAt: Date
}

/// How a LAN pairing ended.
public enum LanPairingOutcome: Sendable, Equatable {
    case approved(BridgeCredentials)
    case denied
    case expired
}
