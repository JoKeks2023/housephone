import CryptoKit
import Foundation
import Security

/// This device's P-256 signing key (ADR-0004). The bridge knows only the
/// public key; nothing that could sign in our name ever leaves the device.
public protocol DeviceSigningKey: Sendable {
    /// X9.63 uncompressed public key (65 bytes).
    var publicKeyX963: Data { get }
    /// ECDSA over SHA-256 of `message`, raw `r ‖ s` (64 bytes).
    func signature(for message: Data) throws -> Data
}

/// A key in the Secure Enclave. It can't be exported or copied, not even
/// through backups.
public struct SecureEnclaveDeviceKey: DeviceSigningKey, @unchecked Sendable {
    // SecureEnclave.P256.Signing.PrivateKey is an immutable value.
    let privateKey: SecureEnclave.P256.Signing.PrivateKey

    public var publicKeyX963: Data { privateKey.publicKey.x963Representation }

    public func signature(for message: Data) throws -> Data {
        try privateKey.signature(for: message).rawRepresentation
    }
}

/// Fallback without a Secure Enclave, and for tests and the probe.
public struct SoftwareDeviceKey: DeviceSigningKey {
    let privateKey: P256.Signing.PrivateKey

    public init() {
        privateKey = P256.Signing.PrivateKey()
    }

    public init(rawRepresentation: Data) throws {
        privateKey = try P256.Signing.PrivateKey(rawRepresentation: rawRepresentation)
    }

    public var publicKeyX963: Data { privateKey.publicKey.x963Representation }

    public func signature(for message: Data) throws -> Data {
        try privateKey.signature(for: message).rawRepresentation
    }
}

public protocol DeviceKeyStore: Sendable {
    /// Creates a new key under `tag`, replacing an existing one.
    func makeKey(tag: String) throws -> any DeviceSigningKey
    func key(tag: String) throws -> (any DeviceSigningKey)?
    func deleteKey(tag: String) throws
}

/// Keeps device keys in the keychain.
///
/// Secure Enclave keys are created with `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`
/// and only `.privateKeyUsage`: signing must work while the phone is
/// locked (an incoming call arrives by VoIP push), so no biometry. The
/// key's `dataRepresentation` is an encrypted blob only this device's
/// Secure Enclave can use; it's stored `ThisDeviceOnly` as well.
public struct KeychainDeviceKeyStore: DeviceKeyStore {
    public let service: String

    public init(service: String = "com.jorisconrad.housephone.devicekey") {
        self.service = service
    }

    private struct Stored: Codable {
        enum Kind: String, Codable {
            case secureEnclave
            case software
        }

        var kind: Kind
        var data: Data
    }

    public func makeKey(tag: String) throws -> any DeviceSigningKey {
        let key: any DeviceSigningKey
        let stored: Stored
        if SecureEnclave.isAvailable {
            var error: Unmanaged<CFError>?
            guard let access = SecAccessControlCreateWithFlags(
                kCFAllocatorDefault,
                kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
                .privateKeyUsage,
                &error
            ) else {
                throw error.map { $0.takeRetainedValue() as any Error } ?? KeychainError(status: errSecParam)
            }
            let privateKey = try SecureEnclave.P256.Signing.PrivateKey(compactRepresentable: false, accessControl: access)
            key = SecureEnclaveDeviceKey(privateKey: privateKey)
            stored = Stored(kind: .secureEnclave, data: privateKey.dataRepresentation)
        } else {
            let software = SoftwareDeviceKey()
            key = software
            stored = Stored(kind: .software, data: software.privateKey.rawRepresentation)
        }
        try save(stored, tag: tag)
        return key
    }

    public func key(tag: String) throws -> (any DeviceSigningKey)? {
        var query = baseQuery(tag: tag)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { return nil }
            let stored = try JSONDecoder().decode(Stored.self, from: data)
            switch stored.kind {
            case .secureEnclave:
                return SecureEnclaveDeviceKey(privateKey: try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: stored.data))
            case .software:
                return try SoftwareDeviceKey(rawRepresentation: stored.data)
            }
        case errSecItemNotFound:
            return nil
        default:
            throw KeychainError(status: status)
        }
    }

    public func deleteKey(tag: String) throws {
        let status = SecItemDelete(baseQuery(tag: tag) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw KeychainError(status: status) }
    }

    private func baseQuery(tag: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: tag,
        ]
    }

    private func save(_ stored: Stored, tag: String) throws {
        let attributes: [String: Any] = [
            kSecValueData as String: try JSONEncoder().encode(stored),
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let status = SecItemUpdate(baseQuery(tag: tag) as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            let added = SecItemAdd(baseQuery(tag: tag).merging(attributes) { $1 } as CFDictionary, nil)
            guard added == errSecSuccess else { throw KeychainError(status: added) }
        } else if status != errSecSuccess {
            throw KeychainError(status: status)
        }
    }
}

/// Software keys in memory, for tests and SwiftUI previews.
public final class InMemoryDeviceKeyStore: DeviceKeyStore, @unchecked Sendable {
    private let lock = NSLock()
    private var keys: [String: SoftwareDeviceKey] = [:]

    public init() {}

    public func makeKey(tag: String) throws -> any DeviceSigningKey {
        let key = SoftwareDeviceKey()
        lock.withLock { keys[tag] = key }
        return key
    }

    public func key(tag: String) throws -> (any DeviceSigningKey)? {
        lock.withLock { keys[tag] }
    }

    public func deleteKey(tag: String) throws {
        _ = lock.withLock { keys.removeValue(forKey: tag) }
    }

    /// Stores a known key, e.g. from the test vectors.
    public func set(_ key: SoftwareDeviceKey, tag: String) {
        lock.withLock { keys[tag] = key }
    }

    public var tags: [String] {
        lock.withLock { Array(keys.keys) }
    }
}
