import CryptoKit
import Foundation
import Security

public enum SealedPushError: Error, Equatable {
    /// Not a `{"sealed": "…"}` body, or not base64url.
    case malformed
    /// Authentication failed: wrong key or tampered payload.
    case cannotOpen
}

/// End-to-end sealed VoIP push (ADR-0010). The bridge encrypts the
/// `IncomingCallPush` for this device's push key, so neither the push
/// relay nor Apple can read caller number or name.
///
///     shared = X25519(ephemeral, push key)
///     key    = HKDF-SHA256(shared, salt = ephemeral pub ‖ push key pub,
///                          info = "housephone-push-v1", 32 bytes)
///     sealed = ephemeral pub (32) ‖ ChaChaPoly(key, nonce = 12 zero bytes,
///                                              aad = "housephone-push-v1")
public enum SealedPush {
    public static let info = "housephone-push-v1"

    /// The `sealed` field of a push body, or `nil` for a plaintext push.
    public static func sealedField(in dictionary: [AnyHashable: Any]) -> String? {
        dictionary["sealed"] as? String
    }

    /// Decrypts a base64url `sealed` value with the device's push key.
    public static func open(_ sealed: String, with key: Curve25519.KeyAgreement.PrivateKey) throws -> Data {
        guard let raw = HP2.data(base64URL: sealed) else { throw SealedPushError.malformed }
        return try open(raw, with: key)
    }

    public static func open(_ sealed: Data, with key: Curve25519.KeyAgreement.PrivateKey) throws -> Data {
        let keySize = 32
        let tagSize = 16
        guard sealed.count >= keySize + tagSize else { throw SealedPushError.malformed }
        let ephemeralPublic = Data(sealed.prefix(keySize))
        let box = Data(sealed.dropFirst(keySize))
        do {
            let ephemeral = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: ephemeralPublic)
            let shared = try key.sharedSecretFromKeyAgreement(with: ephemeral)
            let symmetric = shared.hkdfDerivedSymmetricKey(
                using: SHA256.self,
                salt: ephemeralPublic + key.publicKey.rawRepresentation,
                sharedInfo: Data(info.utf8),
                outputByteCount: 32
            )
            let sealedBox = try ChaChaPoly.SealedBox(
                nonce: ChaChaPoly.Nonce(data: Data(count: 12)),
                ciphertext: box.dropLast(tagSize),
                tag: box.suffix(tagSize)
            )
            return try ChaChaPoly.open(sealedBox, using: symmetric, authenticating: Data(info.utf8))
        } catch {
            throw SealedPushError.cannotOpen
        }
    }
}

extension IncomingCallPush {
    /// Parses `PKPushPayload.dictionaryPayload`: a sealed push (ADR-0010)
    /// opened with `pushKey`, or a plaintext one (own APNs key, older
    /// bridges).
    public init(dictionary: [AnyHashable: Any], pushKey: Curve25519.KeyAgreement.PrivateKey?) throws {
        if let sealed = SealedPush.sealedField(in: dictionary) {
            guard let pushKey else { throw SealedPushError.cannotOpen }
            try self.init(data: try SealedPush.open(sealed, with: pushKey))
        } else {
            try self.init(dictionary: dictionary)
        }
    }
}

/// Stores the device's X25519 push key (ADR-0010) in the keychain: created
/// once per installation, `AfterFirstUnlockThisDeviceOnly` and without
/// biometry, because pushes arrive while the phone is locked.
public struct PushKeyStore: Sendable {
    public let service: String
    public let account: String

    public init(service: String = "com.jorisconrad.housephone.pushkey", account: String = "push-key") {
        self.service = service
        self.account = account
    }

    /// The stored key, created on first use.
    public func loadOrCreate() throws -> Curve25519.KeyAgreement.PrivateKey {
        if let existing = try load() { return existing }
        let key = Curve25519.KeyAgreement.PrivateKey()
        let attributes: [String: Any] = [
            kSecValueData as String: key.rawRepresentation,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let status = SecItemAdd(baseQuery.merging(attributes) { $1 } as CFDictionary, nil)
        if status == errSecDuplicateItem, let raced = try load() { return raced }
        guard status == errSecSuccess else { throw KeychainError(status: status) }
        return key
    }

    public func load() throws -> Curve25519.KeyAgreement.PrivateKey? {
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { return nil }
            return try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: data)
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainError(status: status)
        }
    }

    public func delete() throws {
        let status = SecItemDelete(baseQuery as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw KeychainError(status: status) }
    }

    private var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }
}

extension Curve25519.KeyAgreement.PrivateKey {
    /// The public key as sent in `hello.pushKey`: base64url, no padding.
    public var pushKeyWireValue: String { HP2.base64URL(publicKey.rawRepresentation) }
}

extension PushKeyStore {
    /// The value for `hello.pushKey`, creating the key on first use; `nil`
    /// if the keychain is unavailable (the bridge keeps the old one).
    public func wirePublicKey() -> String? {
        try? loadOrCreate().pushKeyWireValue
    }
}

extension IncomingCallPush {
    /// Like `init(dictionary:pushKey:)`, reading the push key from `store`
    /// only for sealed pushes.
    public init(dictionary: [AnyHashable: Any], pushKeyStore store: PushKeyStore) throws {
        let key = SealedPush.sealedField(in: dictionary) == nil ? nil : try? store.load()
        try self.init(dictionary: dictionary, pushKey: key)
    }
}
